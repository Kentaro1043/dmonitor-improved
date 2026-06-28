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

static int (*real_open_fn)(const char *, int, ...) = NULL;
static int (*real_openat_fn)(int, const char *, int, ...) = NULL;
static FILE *(*real_fopen_fn)(const char *, const char *) = NULL;
static void *(*real_mmap_fn)(void *, size_t, int, int, int, off_t) = NULL;
static int (*real_system_fn)(const char *) = NULL;

static void load_symbols(void) {
  if (!real_open_fn) {
    real_open_fn = dlsym(RTLD_NEXT, "open");
    real_openat_fn = dlsym(RTLD_NEXT, "openat");
    real_fopen_fn = dlsym(RTLD_NEXT, "fopen");
    real_mmap_fn = dlsym(RTLD_NEXT, "mmap");
    real_system_fn = dlsym(RTLD_NEXT, "system");
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

static char *rewrite_path(const char *path) {
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

static int open_cpuinfo(void) {
  load_symbols();
#ifdef O_TMPFILE
  int tmp_fd = real_open_fn("/tmp", O_RDWR | O_TMPFILE, 0600);
  if (tmp_fd >= 0) {
    write_all(tmp_fd, cpuinfo_text);
    lseek(tmp_fd, 0, SEEK_SET);
    return tmp_fd;
  }
#endif
  char tmpl[] = "/tmp/dmonitor-cpuinfo-XXXXXX";
  int fd = mkstemp(tmpl);
  if (fd < 0) {
    return fd;
  }
  unlink(tmpl);
  write_all(fd, cpuinfo_text);
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
    return open_cpuinfo();
  }
  if (strcmp(pathname, "/dev/mem") == 0 || strcmp(pathname, "/dev/gpiomem") == 0) {
    return real_open_fn("/dev/zero", flags, mode);
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
      return open_cpuinfo();
    }
    if (strcmp(pathname, "/dev/mem") == 0 || strcmp(pathname, "/dev/gpiomem") == 0) {
      return real_open_fn("/dev/zero", flags, mode);
    }
  }
  return real_openat_fn(dirfd, pathname, flags, mode);
}

FILE *fopen(const char *pathname, const char *mode) {
  load_symbols();
  if (pathname && strcmp(pathname, "/proc/cpuinfo") == 0) {
    return fmemopen((void *)cpuinfo_text, strlen(cpuinfo_text), mode);
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

void *mmap(void *addr, size_t length, int prot, int flags, int fd, off_t offset) {
  load_symbols();
  void *mapped = real_mmap_fn(addr, length, prot, flags, fd, offset);
  if (mapped == MAP_FAILED && errno == EPERM) {
    mapped = real_mmap_fn(addr, length, prot, MAP_PRIVATE | MAP_ANONYMOUS, -1, 0);
  }
  return mapped;
}

int system(const char *command) {
  load_symbols();
  if (command && strstr(command, "/var/www/cgi-bin/")) {
    return 0;
  }
  return real_system_fn(command);
}
