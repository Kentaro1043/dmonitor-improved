import {
  Activity,
  CircleStop,
  Download,
  Minus,
  Play,
  Plus,
  Power,
  Radio,
  RefreshCw,
  Save,
  Search,
  Settings,
  Unplug,
} from "lucide-react";
import { FormEvent, useEffect, useMemo, useState } from "react";
import type { ReactNode } from "react";
import { api, Config, LogEntry, Repeater, Status } from "./api";

const emptyConfig: Config = {
  rig: "ICOM",
  lcd: "NONE",
  callsign: "",
  gpsSkipMode: "NO_SKIP",
};

export default function App() {
  const [status, setStatus] = useState<Status | null>(null);
  const [logs, setLogs] = useState<LogEntry[]>([]);
  const [config, setConfig] = useState<Config>(emptyConfig);
  const [selected, setSelected] = useState<Repeater | null>(null);
  const [manual, setManual] = useState({
    address: "",
    port: "51000",
    areaCallsign: "",
    zoneCallsign: "",
  });
  const [busy, setBusy] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  const repeaters = status?.runtime.repeaters ?? [];
  const processes = status?.runtime.processes ?? {};

  async function refresh() {
    const [nextStatus, nextLogs] = await Promise.all([
      api.status(),
      api.logs(),
    ]);
    setStatus(nextStatus);
    setConfig(nextStatus.config);
    setLogs(nextLogs.logs.slice(-80).reverse());
  }

  async function run(label: string, action: () => Promise<unknown>) {
    setBusy(label);
    setError(null);
    try {
      await action();
      await refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(null);
    }
  }

  useEffect(() => {
    refresh().catch((err) =>
      setError(err instanceof Error ? err.message : String(err)),
    );
    const id = window.setInterval(() => refresh().catch(() => undefined), 5000);
    return () => window.clearInterval(id);
  }, []);

  useEffect(() => {
    if (!selected) return;
    setManual({
      address: selected.address ?? "",
      port: selected.port ?? "51000",
      areaCallsign: selected.areaCallsign,
      zoneCallsign: selected.zoneCallsign ?? "",
    });
  }, [selected]);

  const runningCount = useMemo(
    () => Object.values(processes).filter((p) => p.running).length,
    [processes],
  );

  function submitConfig(event: FormEvent) {
    event.preventDefault();
    void run("save", () => api.saveConfig(config));
  }

  function connect(event: FormEvent) {
    event.preventDefault();
    void run("connect", () =>
      api.connect({
        connectCallsign: config.callsign,
        address: manual.address,
        port: manual.port || "51000",
        areaCallsign: manual.areaCallsign,
        zoneCallsign: manual.zoneCallsign,
      }),
    );
  }

  return (
    <main className="shell">
      <header className="topbar">
        <div>
          <h1>dmonitor improved</h1>
          <p>{status?.runtime.rootfs ?? "runtime/rootfs"}</p>
        </div>
        <button
          className="iconButton"
          title="更新"
          onClick={() => void run("refresh", refresh)}
          disabled={busy !== null}
        >
          <RefreshCw size={18} />
        </button>
      </header>

      {error && <div className="alert">{error}</div>}

      <section className="metrics">
        <Metric
          icon={<Radio size={18} />}
          label="Device"
          value={
            status?.device.dstarExists
              ? "/dev/dstar"
              : status?.device.ttyACM0Exists
                ? "/dev/ttyACM0"
                : "not found"
          }
          tone={status?.device.dstarExists ? "ok" : "warn"}
        />
        <Metric
          icon={<Activity size={18} />}
          label="Processes"
          value={`${runningCount} running`}
          tone={runningCount > 0 ? "ok" : "idle"}
        />
        <Metric
          icon={<Power size={18} />}
          label="Connection"
          value={status?.runtime.connection?.areaCallsign ?? "standby"}
          tone={status?.runtime.connection ? "ok" : "idle"}
        />
        <Metric
          icon={<Search size={18} />}
          label="Repeaters"
          value={`${repeaters.length}`}
          tone={repeaters.length > 0 ? "ok" : "idle"}
        />
      </section>

      <section className="grid">
        <div className="panel operations">
          <div className="panelTitle">
            <Activity size={18} />
            <h2>Runtime</h2>
          </div>
          <div className="buttonGrid">
            <Action
              icon={<Play size={16} />}
              label="rpt_conn"
              busy={busy}
              onClick={() =>
                run("rpt", () => api.post("/api/runtime/start-rpt-conn"))
              }
            />
            <Action
              icon={<CircleStop size={16} />}
              label="stop rpt"
              busy={busy}
              onClick={() =>
                run("stop-rpt", () => api.post("/api/runtime/stop-rpt-conn"))
              }
            />
            <Action
              icon={<Search size={16} />}
              label="scan"
              busy={busy}
              onClick={() =>
                run("scan", () => api.post("/api/repeater/scan/start"))
              }
            />
            <Action
              icon={<CircleStop size={16} />}
              label="stop scan"
              busy={busy}
              onClick={() =>
                run("stop-scan", () => api.post("/api/repeater/scan/stop"))
              }
            />
            <Action
              icon={<Unplug size={16} />}
              label="disconnect"
              busy={busy}
              onClick={() =>
                run("disconnect", () => api.post("/api/monitor/disconnect"))
              }
            />
            <Action
              icon={<Download size={16} />}
              label="update list"
              busy={busy}
              onClick={() =>
                run("update", () => api.post("/api/repeater/update"))
              }
            />
            <Action
              icon={<Plus size={16} />}
              label="buffer"
              busy={busy}
              onClick={() =>
                run("buffer-plus", () => api.post("/api/buffer/increase"))
              }
            />
            <Action
              icon={<Minus size={16} />}
              label="buffer"
              busy={busy}
              onClick={() =>
                run("buffer-minus", () => api.post("/api/buffer/decrease"))
              }
            />
          </div>
          <ProcessTable processes={Object.values(processes)} />
          <p className="hint">{status?.device.message}</p>
          {!status?.device.dstarExists && (
            <p className="hint">{status?.udevHint}</p>
          )}
        </div>

        <form className="panel connect" onSubmit={connect}>
          <div className="panelTitle">
            <Radio size={18} />
            <h2>Connect</h2>
          </div>
          <label>
            Address
            <input
              value={manual.address}
              onChange={(e) =>
                setManual({ ...manual, address: e.target.value })
              }
              required
            />
          </label>
          <label>
            Port
            <input
              value={manual.port}
              onChange={(e) => setManual({ ...manual, port: e.target.value })}
            />
          </label>
          <label>
            Area callsign
            <input
              value={manual.areaCallsign}
              onChange={(e) =>
                setManual({
                  ...manual,
                  areaCallsign: e.target.value.toUpperCase(),
                })
              }
              required
            />
          </label>
          <label>
            Zone callsign
            <input
              value={manual.zoneCallsign}
              onChange={(e) =>
                setManual({
                  ...manual,
                  zoneCallsign: e.target.value.toUpperCase(),
                })
              }
            />
          </label>
          <button className="primary" disabled={busy !== null}>
            <Play size={16} />
            Connect
          </button>
        </form>

        <form className="panel settings" onSubmit={submitConfig}>
          <div className="panelTitle">
            <Settings size={18} />
            <h2>Config</h2>
          </div>
          <label>
            Rig
            <select
              value={config.rig}
              onChange={(e) => setConfig({ ...config, rig: e.target.value })}
            >
              <option>ICOM</option>
            </select>
          </label>
          <label>
            LCD
            <select
              value={config.lcd}
              onChange={(e) => setConfig({ ...config, lcd: e.target.value })}
            >
              <option>NONE</option>
            </select>
          </label>
          <label>
            Callsign
            <input
              value={config.callsign}
              onChange={(e) =>
                setConfig({ ...config, callsign: e.target.value.toUpperCase() })
              }
              maxLength={8}
            />
          </label>
          <label className="toggle">
            <input
              type="checkbox"
              checked={config.gpsSkipMode === "SKIP"}
              onChange={(e) =>
                setConfig({
                  ...config,
                  gpsSkipMode: e.target.checked ? "SKIP" : "NO_SKIP",
                })
              }
            />
            GPS skip
          </label>
          <button className="primary" disabled={busy !== null}>
            <Save size={16} />
            Save
          </button>
        </form>

        <div className="panel repeaters">
          <div className="panelTitle">
            <Search size={18} />
            <h2>Repeaters</h2>
          </div>
          <div className="repeaterList">
            {repeaters.map((repeater, idx) => (
              <button
                key={`${repeater.areaCallsign}-${idx}`}
                className={selected === repeater ? "row selected" : "row"}
                onClick={() => setSelected(repeater)}
              >
                <strong>{repeater.areaCallsign || "unknown"}</strong>
                <span>{repeater.address || "no address"}</span>
                <span>{repeater.port || "51000"}</span>
              </button>
            ))}
            {repeaters.length === 0 && (
              <p className="empty">No repeater data</p>
            )}
          </div>
        </div>

        <div className="panel logs">
          <div className="panelTitle">
            <Activity size={18} />
            <h2>Logs</h2>
          </div>
          <div className="logList">
            {logs.map((entry, idx) => (
              <div className="logLine" key={`${entry.time}-${idx}`}>
                <span>{entry.time.replace("T", " ").replace("Z", "")}</span>
                <strong>{entry.source}</strong>
                <pre>{entry.message}</pre>
              </div>
            ))}
            {logs.length === 0 && <p className="empty">No logs</p>}
          </div>
        </div>
      </section>
    </main>
  );
}

function Metric({
  icon,
  label,
  value,
  tone,
}: {
  icon: ReactNode;
  label: string;
  value: string;
  tone: "ok" | "warn" | "idle";
}) {
  return (
    <div className={`metric ${tone}`}>
      {icon}
      <span>{label}</span>
      <strong>{value}</strong>
    </div>
  );
}

function Action({
  icon,
  label,
  busy,
  onClick,
}: {
  icon: ReactNode;
  label: string;
  busy: string | null;
  onClick: () => void;
}) {
  return (
    <button type="button" onClick={onClick} disabled={busy !== null}>
      {icon}
      {label}
    </button>
  );
}

function ProcessTable({
  processes,
}: {
  processes: {
    name: string;
    running: boolean;
    pid?: number;
    exitCode?: number;
  }[];
}) {
  if (processes.length === 0)
    return <p className="empty">No managed processes</p>;
  return (
    <table>
      <tbody>
        {processes.map((process) => (
          <tr key={process.name}>
            <td>{process.name}</td>
            <td>{process.running ? "running" : "stopped"}</td>
            <td>{process.pid ?? process.exitCode ?? ""}</td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}
