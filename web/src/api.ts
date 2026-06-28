export type Config = {
  rig: string;
  lcd: string;
  callsign: string;
  gpsSkipMode: string;
};

export type ProcessState = {
  name: string;
  pid?: number;
  running: boolean;
  startedAt?: string;
  exitedAt?: string;
  exitCode?: number;
  lastError?: string;
};

export type Repeater = {
  areaCallsign: string;
  zoneCallsign?: string;
  address?: string;
  port?: string;
  name?: string;
  raw: string;
};

export type Status = {
  runtime: {
    rootfs: string;
    qemuPath: string;
    processes: Record<string, ProcessState>;
    connection?: {
      connectCallsign: string;
      address: string;
      port: string;
      areaCallsign: string;
      zoneCallsign?: string;
      startedAt: string;
    };
    lastError?: string;
    repeaters?: Repeater[];
  };
  device: {
    dstarExists: boolean;
    dstarTarget?: string;
    ttyACM0Exists: boolean;
    vendorId?: string;
    productId?: string;
    message: string;
  };
  udevHint: string;
  config: Config;
};

export type LogEntry = {
  time: string;
  source: string;
  message: string;
};

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(path, {
    headers: { "Content-Type": "application/json", ...(init?.headers ?? {}) },
    ...init,
  });
  const data = await response.json().catch(() => ({}));
  if (!response.ok) {
    throw new Error(data.error ?? response.statusText);
  }
  return data as T;
}

export const api = {
  status: () => request<Status>("/api/status"),
  logs: () => request<{ logs: LogEntry[] | null }>("/api/logs"),
  saveConfig: (config: Config) =>
    request<Config>("/api/config", {
      method: "PUT",
      body: JSON.stringify(config),
    }),
  post: (path: string) => request<Status>(path, { method: "POST" }),
  connect: (body: {
    connectCallsign?: string;
    address: string;
    port: string;
    areaCallsign: string;
    zoneCallsign?: string;
  }) =>
    request<Status>("/api/monitor/connect", {
      method: "POST",
      body: JSON.stringify(body),
    }),
};
