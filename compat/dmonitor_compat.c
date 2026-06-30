#define _GNU_SOURCE
#include <dlfcn.h>
#include <errno.h>
#include <fcntl.h>
#include <stdarg.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/mman.h>
#include <sys/stat.h>
#include <unistd.h>

static const char cpuinfo_text[] =
    "processor\t: 0\n"
    "model name\t: ARMv7 Processor rev 4 (v7l)\n"
    "BogoMIPS\t: 108.00\n"
    "Features\t: half thumb fastmult vfp edsp neon vfpv3 tls vfpv4 idiva idivt\n"
    "CPU implementer\t: 0x41\n"
    "CPU architecture: 7\n"
    "CPU variant\t: 0x0\n"
    "CPU part\t: 0xd03\n"
    "CPU revision\t: 4\n"
    "\n"
    "Hardware\t: BCM2835\n"
    "Revision\t: a020d3\n"
    "Serial\t\t: 00000000dmonitor\n"
    "Model\t\t: Raspberry Pi 3 Model B Plus Rev 1.3\n";

static const char os_release_text[] =
    "PRETTY_NAME=\"Raspberry Pi OS GNU/Linux 12 (bookworm)\"\n"
    "NAME=\"Raspberry Pi OS GNU/Linux\"\n"
    "VERSION_ID=\"12\"\n"
    "VERSION=\"12 (bookworm)\"\n"
    "VERSION_CODENAME=bookworm\n"
    "ID=raspbian\n"
    "ID_LIKE=debian\n";

static int (*real_open_fn)(const char *, int, ...) = NULL;
static int (*real_openat_fn)(int, const char *, int, ...) = NULL;
static FILE *(*real_fopen_fn)(const char *, const char *) = NULL;
static void *(*real_mmap_fn)(void *, size_t, int, int, int, off_t) = NULL;
static int (*real_system_fn)(const char *) = NULL;
static int (*real_close_fn)(int) = NULL;
static int (*real_stat_fn)(const char *, struct stat *) = NULL;
static int (*real_stat64_fn)(const char *, struct stat64 *) = NULL;
static int (*real_lstat_fn)(const char *, struct stat *) = NULL;
static int (*real_lstat64_fn)(const char *, struct stat64 *) = NULL;
static int (*real_xstat_fn)(int, const char *, struct stat *) = NULL;
static int (*real_xstat64_fn)(int, const char *, struct stat64 *) = NULL;
static int (*real_lxstat_fn)(int, const char *, struct stat *) = NULL;
static int (*real_lxstat64_fn)(int, const char *, struct stat64 *) = NULL;
static int (*real_access_fn)(const char *, int) = NULL;
static int (*real_faccessat_fn)(int, const char *, int, int) = NULL;

static int dummy_gpio_fds[32];

static void load_symbols(void) {
  if (!real_open_fn) {
    real_open_fn = dlsym(RTLD_NEXT, "open");
    real_openat_fn = dlsym(RTLD_NEXT, "openat");
    real_fopen_fn = dlsym(RTLD_NEXT, "fopen");
    real_mmap_fn = dlsym(RTLD_NEXT, "mmap");
    real_system_fn = dlsym(RTLD_NEXT, "system");
    real_close_fn = dlsym(RTLD_NEXT, "close");
    real_stat_fn = dlsym(RTLD_NEXT, "stat");
    real_stat64_fn = dlsym(RTLD_NEXT, "stat64");
    real_lstat_fn = dlsym(RTLD_NEXT, "lstat");
    real_lstat64_fn = dlsym(RTLD_NEXT, "lstat64");
    real_xstat_fn = dlsym(RTLD_NEXT, "__xstat");
    real_xstat64_fn = dlsym(RTLD_NEXT, "__xstat64");
    real_lxstat_fn = dlsym(RTLD_NEXT, "__lxstat");
    real_lxstat64_fn = dlsym(RTLD_NEXT, "__lxstat64");
    real_access_fn = dlsym(RTLD_NEXT, "access");
    real_faccessat_fn = dlsym(RTLD_NEXT, "faccessat");
  }
}

static int starts_with(const char *s, const char *prefix) {
  return s && strncmp(s, prefix, strlen(prefix)) == 0;
}

static void write_all(int fd, const char *text) {
  size_t total = strlen(text);
  const char *cursor = text;
  while (total > 0) {
    ssize_t written = write(fd, cursor, total);
    if (written <= 0) {
      return;
    }
    cursor += written;
    total -= (size_t)written;
  }
}

static void track_dummy_gpio_fd(int fd) {
  if (fd < 0) {
    return;
  }
  for (size_t i = 0; i < sizeof(dummy_gpio_fds) / sizeof(dummy_gpio_fds[0]); i++) {
    if (dummy_gpio_fds[i] == 0) {
      dummy_gpio_fds[i] = fd;
      return;
    }
  }
}

static int is_dummy_gpio_fd(int fd) {
  if (fd < 0) {
    return 0;
  }
  for (size_t i = 0; i < sizeof(dummy_gpio_fds) / sizeof(dummy_gpio_fds[0]); i++) {
    if (dummy_gpio_fds[i] == fd) {
      return 1;
    }
  }
  return 0;
}

static char *dup_string(const char *value) {
  size_t len = strlen(value) + 1;
  char *out = malloc(len);
  if (!out) {
    return NULL;
  }
  memcpy(out, value, len);
  return out;
}

static char *rewrite_dstar_path(const char *path) {
  const char *device = getenv("DMONITOR_DSTAR_DEVICE");
  if (!path || strcmp(path, "/dev/dstar") != 0 || !device || device[0] == '\0' ||
      strcmp(device, "/dev/dstar") == 0) {
    return NULL;
  }
  return dup_string(device);
}

static void untrack_dummy_gpio_fd(int fd) {
  for (size_t i = 0; i < sizeof(dummy_gpio_fds) / sizeof(dummy_gpio_fds[0]); i++) {
    if (dummy_gpio_fds[i] == fd) {
      dummy_gpio_fds[i] = 0;
    }
  }
}

static int open_dummy_gpio(void) {
  load_symbols();
  int fd = real_open_fn("/dev/zero", O_RDWR, 0600);
  track_dummy_gpio_fd(fd);
  return fd;
}

static char *rewrite_path(const char *path) {
  char *device = rewrite_dstar_path(path);
  if (device) {
    return device;
  }

  const char *root = getenv("DMONITOR_HOST_ROOTFS");
  if (!path || !root || root[0] == '\0') {
    return NULL;
  }
  if (strcmp(path, "/proc/cpuinfo") == 0) {
    return NULL;
  }
  if (starts_with(path, "/var/www/") || starts_with(path, "/var/tmp/") ||
      starts_with(path, "/var/log/") || starts_with(path, "/var/run/")) {
    size_t len = strlen(root) + strlen(path) + 1;
    char *out = malloc(len);
    if (!out) {
      return NULL;
    }
    snprintf(out, len, "%s%s", root, path);
    return out;
  }
  return NULL;
}

static int open_text(const char *text) {
  load_symbols();
#ifdef O_TMPFILE
  int tmp_fd = real_open_fn("/tmp", O_RDWR | O_TMPFILE, 0600);
  if (tmp_fd >= 0) {
    write_all(tmp_fd, text);
    lseek(tmp_fd, 0, SEEK_SET);
    return tmp_fd;
  }
#endif
  char tmpl[] = "/tmp/dmonitor-compat-XXXXXX";
  int fd = mkstemp(tmpl);
  if (fd < 0) {
    return fd;
  }
  unlink(tmpl);
  write_all(fd, text);
  lseek(fd, 0, SEEK_SET);
  return fd;
}

int open(const char *pathname, int flags, ...) {
  load_symbols();
  mode_t mode = 0;
  if (flags & O_CREAT) {
    va_list ap;
    va_start(ap, flags);
    mode = (mode_t)va_arg(ap, int);
    va_end(ap);
  }
  if (strcmp(pathname, "/proc/cpuinfo") == 0) {
    return open_text(cpuinfo_text);
  }
  if (strcmp(pathname, "/etc/os-release") == 0) {
    return open_text(os_release_text);
  }
  if (strcmp(pathname, "/dev/mem") == 0 || strcmp(pathname, "/dev/gpiomem") == 0) {
    return open_dummy_gpio();
  }
  char *rewritten = rewrite_path(pathname);
  if (rewritten) {
    int fd = real_open_fn(rewritten, flags, mode);
    free(rewritten);
    return fd;
  }
  return real_open_fn(pathname, flags, mode);
}

int open64(const char *pathname, int flags, ...) {
  mode_t mode = 0;
  if (flags & O_CREAT) {
    va_list ap;
    va_start(ap, flags);
    mode = (mode_t)va_arg(ap, int);
    va_end(ap);
  }
  return open(pathname, flags, mode);
}

int openat(int dirfd, const char *pathname, int flags, ...) {
  load_symbols();
  mode_t mode = 0;
  if (flags & O_CREAT) {
    va_list ap;
    va_start(ap, flags);
    mode = (mode_t)va_arg(ap, int);
    va_end(ap);
  }
  if (pathname[0] == '/') {
    char *rewritten = rewrite_path(pathname);
    if (rewritten) {
      int fd = real_open_fn(rewritten, flags, mode);
      free(rewritten);
      return fd;
    }
    if (strcmp(pathname, "/proc/cpuinfo") == 0) {
      return open_text(cpuinfo_text);
    }
    if (strcmp(pathname, "/etc/os-release") == 0) {
      return open_text(os_release_text);
    }
    if (strcmp(pathname, "/dev/mem") == 0 || strcmp(pathname, "/dev/gpiomem") == 0) {
      return open_dummy_gpio();
    }
  }
  return real_openat_fn(dirfd, pathname, flags, mode);
}

FILE *fopen(const char *pathname, const char *mode) {
  load_symbols();
  if (pathname && strcmp(pathname, "/proc/cpuinfo") == 0) {
    return fmemopen((void *)cpuinfo_text, strlen(cpuinfo_text), mode);
  }
  if (pathname && strcmp(pathname, "/etc/os-release") == 0) {
    return fmemopen((void *)os_release_text, strlen(os_release_text), mode);
  }
  char *rewritten = rewrite_path(pathname);
  if (rewritten) {
    FILE *f = real_fopen_fn(rewritten, mode);
    free(rewritten);
    return f;
  }
  return real_fopen_fn(pathname, mode);
}

FILE *fopen64(const char *pathname, const char *mode) {
  return fopen(pathname, mode);
}

int stat(const char *pathname, struct stat *statbuf) {
  load_symbols();
  char *rewritten = rewrite_path(pathname);
  if (rewritten) {
    int ret = real_stat_fn(rewritten, statbuf);
    free(rewritten);
    return ret;
  }
  return real_stat_fn(pathname, statbuf);
}

int stat64(const char *pathname, struct stat64 *statbuf) {
  load_symbols();
  char *rewritten = rewrite_path(pathname);
  if (rewritten) {
    int ret = real_stat64_fn(rewritten, statbuf);
    free(rewritten);
    return ret;
  }
  return real_stat64_fn(pathname, statbuf);
}

int lstat(const char *pathname, struct stat *statbuf) {
  load_symbols();
  char *rewritten = rewrite_path(pathname);
  if (rewritten) {
    int ret = real_lstat_fn(rewritten, statbuf);
    free(rewritten);
    return ret;
  }
  return real_lstat_fn(pathname, statbuf);
}

int lstat64(const char *pathname, struct stat64 *statbuf) {
  load_symbols();
  char *rewritten = rewrite_path(pathname);
  if (rewritten) {
    int ret = real_lstat64_fn(rewritten, statbuf);
    free(rewritten);
    return ret;
  }
  return real_lstat64_fn(pathname, statbuf);
}

int __xstat(int ver, const char *pathname, struct stat *statbuf) {
  load_symbols();
  char *rewritten = rewrite_path(pathname);
  if (rewritten) {
    int ret = real_xstat_fn(ver, rewritten, statbuf);
    free(rewritten);
    return ret;
  }
  return real_xstat_fn(ver, pathname, statbuf);
}

int __xstat64(int ver, const char *pathname, struct stat64 *statbuf) {
  load_symbols();
  char *rewritten = rewrite_path(pathname);
  if (rewritten) {
    int ret = real_xstat64_fn(ver, rewritten, statbuf);
    free(rewritten);
    return ret;
  }
  return real_xstat64_fn(ver, pathname, statbuf);
}

int __lxstat(int ver, const char *pathname, struct stat *statbuf) {
  load_symbols();
  char *rewritten = rewrite_path(pathname);
  if (rewritten) {
    int ret = real_lxstat_fn(ver, rewritten, statbuf);
    free(rewritten);
    return ret;
  }
  return real_lxstat_fn(ver, pathname, statbuf);
}

int __lxstat64(int ver, const char *pathname, struct stat64 *statbuf) {
  load_symbols();
  char *rewritten = rewrite_path(pathname);
  if (rewritten) {
    int ret = real_lxstat64_fn(ver, rewritten, statbuf);
    free(rewritten);
    return ret;
  }
  return real_lxstat64_fn(ver, pathname, statbuf);
}

int access(const char *pathname, int mode) {
  load_symbols();
  char *rewritten = rewrite_path(pathname);
  if (rewritten) {
    int ret = real_access_fn(rewritten, mode);
    free(rewritten);
    return ret;
  }
  return real_access_fn(pathname, mode);
}

int faccessat(int dirfd, const char *pathname, int mode, int flags) {
  load_symbols();
  if (pathname[0] == '/') {
    char *rewritten = rewrite_path(pathname);
    if (rewritten) {
      int ret = real_faccessat_fn(AT_FDCWD, rewritten, mode, flags);
      free(rewritten);
      return ret;
    }
  }
  return real_faccessat_fn(dirfd, pathname, mode, flags);
}

void *mmap(void *addr, size_t length, int prot, int flags, int fd, off_t offset) {
  load_symbols();
  if (is_dummy_gpio_fd(fd)) {
    return real_mmap_fn(addr, length, prot, MAP_PRIVATE | MAP_ANONYMOUS, -1, 0);
  }
  void *mapped = real_mmap_fn(addr, length, prot, flags, fd, offset);
  if (mapped == MAP_FAILED && errno == EPERM) {
    mapped = real_mmap_fn(addr, length, prot, MAP_PRIVATE | MAP_ANONYMOUS, -1, 0);
  }
  return mapped;
}

int close(int fd) {
  load_symbols();
  untrack_dummy_gpio_fd(fd);
  return real_close_fn(fd);
}

int system(const char *command) {
  load_symbols();
  if (command && strstr(command, "/var/www/cgi-bin/")) {
    return 0;
  }
  return real_system_fn(command);
}
