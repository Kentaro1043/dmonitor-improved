import {
  Accordion,
  ActionIcon,
  Alert,
  Badge,
  Button,
  Card,
  Container,
  Group,
  Paper,
  ScrollArea,
  Select,
  SimpleGrid,
  Stack,
  Switch,
  Table,
  Text,
  TextInput,
  ThemeIcon,
  Title,
  Tooltip,
} from "@mantine/core";
import {
  Activity,
  CircleStop,
  Download,
  ExternalLink,
  FileText,
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
import { useRef } from "react";
import type { ReactNode } from "react";
import { api, Config, Repeater, Status } from "./api";

const DSTAR_OPERATION_LOG_URL = "http://log.d-star.info/usr/log_view.html";

const emptyConfig: Config = {
  rig: "ICOM",
  lcd: "NONE",
  callsign: "",
  gpsSkipMode: "NO_SKIP",
};

export default function App() {
  const path = normalizePath(window.location.pathname);
  if (path === "/dstar-log") {
    return <DStarOperationLogPage />;
  }
  if (path === "/trust-access-log") {
    return <TrustAccessLogPage />;
  }
  return <MainApp />;
}

function MainApp() {
  const [status, setStatus] = useState<Status | null>(null);
  const [config, setConfig] = useState<Config>(emptyConfig);
  const configDirty = useRef(false);
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
  const activeRepeaters = status?.runtime.activeRepeaters ?? [];
  const processes = status?.runtime.processes ?? {};
  const processRows = Object.values(processes);

  async function refresh(options: { syncConfig?: boolean } = {}) {
    const nextStatus = await api.status();
    setStatus(nextStatus);
    if (options.syncConfig || !configDirty.current) {
      setConfig(nextStatus.config);
    }
  }

  function updateConfig(patch: Partial<Config>) {
    configDirty.current = true;
    setConfig((current) => ({ ...current, ...patch }));
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
    refresh({ syncConfig: true }).catch((err) =>
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
    () => processRows.filter((process) => process.running).length,
    [processRows],
  );
  const repeaterGroups = useMemo(
    () => groupRepeatersByArea(repeaters),
    [repeaters],
  );
  const utilityPages = [
    {
      id: "dstar-log",
      icon: <FileText size={16} />,
      label: "運用ログ",
      description: "全国のD-STAR運用ログを別画面で開く",
      path: "/dstar-log",
    },
    {
      id: "trust-access-log",
      icon: <ExternalLink size={16} />,
      label: "テーブル要求",
      description: "管理サーバーへのテーブル書き換え要求を別画面で開く",
      path: "/trust-access-log",
    },
  ];

  function submitConfig(event: FormEvent) {
    event.preventDefault();
    void run("save", async () => {
      const saved = await api.saveConfig(config);
      configDirty.current = false;
      setConfig(saved);
    });
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

  const metrics = [
    {
      icon: <Radio size={18} />,
      label: "Device",
      value: status?.device.dstarExists
        ? (status.device.dstarTarget ?? status.device.devicePath)
        : status?.device.ttyACM0Exists
          ? "/dev/ttyACM0"
          : "not found",
      color: status?.device.dstarExists ? "teal" : "orange",
    },
    {
      icon: <Activity size={18} />,
      label: "Processes",
      value: `${runningCount} running`,
      color: runningCount > 0 ? "teal" : "gray",
    },
    {
      icon: <Power size={18} />,
      label: "Connection",
      value: status?.runtime.connection?.areaCallsign ?? "standby",
      color: status?.runtime.connection ? "teal" : "gray",
    },
    {
      icon: <Search size={18} />,
      label: "Repeaters",
      value: `${repeaters.length} / ${activeRepeaters.length} active`,
      color: repeaters.length > 0 ? "teal" : "gray",
    },
  ];
  const runtimeActions = [
    {
      id: "rpt",
      icon: <Play size={16} />,
      label: "待受を開始",
      description: "レピーター接続待受と状態監視を開始",
      action: () => api.post("/api/runtime/start-rpt-conn"),
    },
    {
      id: "stop-rpt",
      icon: <CircleStop size={16} />,
      label: "待受を停止",
      description: "待受とレピーター状態監視を停止",
      action: () => api.post("/api/runtime/stop-rpt-conn"),
    },
    {
      id: "scan",
      icon: <Search size={16} />,
      label: "スキャン開始",
      description: "レピーター探索を開始",
      action: () => api.post("/api/repeater/scan/start"),
    },
    {
      id: "stop-scan",
      icon: <CircleStop size={16} />,
      label: "スキャン停止",
      description: "レピーター探索を停止",
      action: () => api.post("/api/repeater/scan/stop"),
    },
    {
      id: "disconnect",
      icon: <Unplug size={16} />,
      label: "接続を切断",
      description: "現在の dmonitor 接続を終了して待受へ戻す",
      action: () => api.post("/api/monitor/disconnect"),
    },
    {
      id: "update",
      icon: <Download size={16} />,
      label: "一覧を更新",
      description: "公式レピーター一覧を再取得",
      action: () => api.post("/api/repeater/update"),
    },
    {
      id: "buffer-plus",
      icon: <Plus size={16} />,
      label: "バッファ +",
      description: "dmonitor の受信バッファを増やす",
      action: () => api.post("/api/buffer/increase"),
    },
    {
      id: "buffer-minus",
      icon: <Minus size={16} />,
      label: "バッファ -",
      description: "dmonitor の受信バッファを減らす",
      action: () => api.post("/api/buffer/decrease"),
    },
  ];

  return (
    <Container fluid py="md" px={{ base: "sm", md: "lg" }}>
      <Stack gap="md">
        <Group justify="space-between" align="flex-start" className="appHeader">
          <div className="appTitleBlock">
            <Title order={1} size="h2">
              dmonitor improved
            </Title>
            <Text size="sm" c="dimmed">
              {status?.runtime.rootfs ?? "runtime/rootfs"}
            </Text>
          </div>
          <Group gap="xs" justify="flex-end" className="navbarActions">
            {utilityPages.map((item) => (
              <Tooltip key={item.id} label={item.description} openDelay={300}>
                <Button
                  variant="default"
                  size="sm"
                  leftSection={item.icon}
                  onClick={() => openStandalonePage(item.path)}
                >
                  {item.label}
                </Button>
              </Tooltip>
            ))}
            <Tooltip label="更新">
              <ActionIcon
                variant="default"
                size="lg"
                onClick={() => void run("refresh", refresh)}
                disabled={busy !== null}
              >
                <RefreshCw size={18} />
              </ActionIcon>
            </Tooltip>
          </Group>
        </Group>

        {error && (
          <Alert color="red" variant="light">
            {error}
          </Alert>
        )}

        <SimpleGrid cols={{ base: 1, sm: 2, lg: 4 }}>
          {metrics.map((metric) => (
            <Metric key={metric.label} {...metric} />
          ))}
        </SimpleGrid>

        <div className="workspaceGrid">
          <Stack gap="md" className="controlPane">
            <Panel title="Operations" icon={<Activity size={18} />}>
              <SimpleGrid cols={2} spacing="xs">
                {runtimeActions.map((item) => (
                  <Action
                    key={item.id}
                    icon={item.icon}
                    label={item.label}
                    description={item.description}
                    busy={busy}
                    onClick={() => run(item.id, item.action)}
                  />
                ))}
              </SimpleGrid>
              <ProcessTable processes={processRows} />
              <Text size="xs" c="dimmed">
                {status?.device.message}
              </Text>
              {!status?.device.dstarExists && (
                <Text size="xs" c="dimmed">
                  {status?.udevHint}
                </Text>
              )}
            </Panel>

            <Paper
              component="form"
              withBorder
              p="md"
              radius="md"
              onSubmit={connect}
            >
              <PanelHeader icon={<Radio size={18} />} title="Connect" />
              <Stack gap="xs">
                <TextInput
                  label="Address"
                  value={manual.address}
                  onChange={(event) =>
                    setManual({ ...manual, address: event.currentTarget.value })
                  }
                  required
                />
                <TextInput
                  label="Port"
                  value={manual.port}
                  onChange={(event) =>
                    setManual({ ...manual, port: event.currentTarget.value })
                  }
                />
                <TextInput
                  label="Area callsign"
                  value={manual.areaCallsign}
                  onChange={(event) =>
                    setManual({
                      ...manual,
                      areaCallsign: event.currentTarget.value.toUpperCase(),
                    })
                  }
                  required
                />
                <TextInput
                  label="Zone callsign"
                  value={manual.zoneCallsign}
                  onChange={(event) =>
                    setManual({
                      ...manual,
                      zoneCallsign: event.currentTarget.value.toUpperCase(),
                    })
                  }
                />
                <Button
                  type="submit"
                  leftSection={<Play size={16} />}
                  disabled={busy !== null}
                >
                  Connect
                </Button>
              </Stack>
            </Paper>

            <Paper
              component="form"
              withBorder
              p="md"
              radius="md"
              onSubmit={submitConfig}
            >
              <PanelHeader icon={<Settings size={18} />} title="Config" />
              <Stack gap="xs">
                <Select
                  label="Rig"
                  data={["ICOM"]}
                  value={config.rig}
                  onChange={(value) => updateConfig({ rig: value ?? "ICOM" })}
                />
                <Select
                  label="LCD"
                  data={["NONE"]}
                  value={config.lcd}
                  onChange={(value) => updateConfig({ lcd: value ?? "NONE" })}
                />
                <TextInput
                  label="Callsign"
                  value={config.callsign}
                  maxLength={8}
                  onChange={(event) =>
                    updateConfig({
                      callsign: event.currentTarget.value.toUpperCase(),
                    })
                  }
                />
                <Switch
                  label="GPS skip"
                  checked={config.gpsSkipMode === "SKIP"}
                  onChange={(event) =>
                    updateConfig({
                      gpsSkipMode: event.currentTarget.checked
                        ? "SKIP"
                        : "NO_SKIP",
                    })
                  }
                />
                <Button
                  type="submit"
                  leftSection={<Save size={16} />}
                  disabled={busy !== null}
                >
                  Save
                </Button>
              </Stack>
            </Paper>
          </Stack>

          <Panel title="Repeaters" icon={<Search size={18} />}>
            <Stack gap="sm">
              {activeRepeaters.length > 0 && (
                <ScrollArea type="auto" offsetScrollbars>
                  <Group wrap="nowrap" gap="xs" className="activeRepeaterRow">
                    {activeRepeaters.map((repeater, idx) => (
                      <Card
                        key={`active-${repeater.areaCallsign}-${idx}`}
                        withBorder
                        padding="xs"
                        radius="sm"
                        miw={150}
                        component="button"
                        onClick={() => setSelected(repeater)}
                      >
                        <Text fw={700} size="xs">
                          {repeater.areaCallsign}
                        </Text>
                        <Text size="xs" c="dimmed" truncate>
                          {displayRepeaterName(repeater)}
                        </Text>
                      </Card>
                    ))}
                  </Group>
                </ScrollArea>
              )}

              {repeaterGroups.length === 0 ? (
                <Text size="sm" c="dimmed">
                  No repeater data
                </Text>
              ) : (
                <Accordion
                  multiple
                  defaultValue={repeaterGroups.map((group) => group.area)}
                  className="repeaterAccordion"
                >
                  {repeaterGroups.map((group) => (
                    <Accordion.Item value={group.area} key={group.area}>
                      <Accordion.Control>
                        <Group justify="space-between" gap="xs" pr={4}>
                          <Text fw={700} size="xs">
                            {areaLabel(group.area)}
                          </Text>
                          <Badge variant="light" size="xs">
                            {group.repeaters.length}
                          </Badge>
                        </Group>
                      </Accordion.Control>
                      <Accordion.Panel>
                        <div className="repeaterGrid">
                          {group.repeaters.map((repeater, idx) => (
                            <RepeaterCard
                              key={`${repeater.areaCallsign}-${idx}`}
                              repeater={repeater}
                              selected={selected === repeater}
                              onClick={() => setSelected(repeater)}
                            />
                          ))}
                        </div>
                      </Accordion.Panel>
                    </Accordion.Item>
                  ))}
                </Accordion>
              )}
            </Stack>
          </Panel>
        </div>
      </Stack>
    </Container>
  );
}

function DStarOperationLogPage() {
  return (
    <UtilityPage
      title="全国D-STAR運用ログ"
      actions={
        <Tooltip label="外部ページを開く">
          <ActionIcon
            component="a"
            href={DSTAR_OPERATION_LOG_URL}
            target="_blank"
            rel="noreferrer"
            variant="default"
            size="lg"
          >
            <ExternalLink size={18} />
          </ActionIcon>
        </Tooltip>
      }
    >
      <iframe
        className="utilityFrame"
        src={DSTAR_OPERATION_LOG_URL}
        title="全国D-STAR運用ログ"
      />
    </UtilityPage>
  );
}

function TrustAccessLogPage() {
  const [log, setLog] = useState("");
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  async function refreshTrustAccessLog() {
    try {
      const text = await api.trustAccessLog();
      setLog(text);
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    void refreshTrustAccessLog();
    const id = window.setInterval(() => void refreshTrustAccessLog(), 5000);
    return () => window.clearInterval(id);
  }, []);

  return (
    <UtilityPage
      title="テーブル書き換え要求"
      actions={
        <Tooltip label="更新">
          <ActionIcon
            variant="default"
            size="lg"
            onClick={() => void refreshTrustAccessLog()}
          >
            <RefreshCw size={18} />
          </ActionIcon>
        </Tooltip>
      }
    >
      <Paper withBorder radius="md" p="md" className="trustLogPanel">
        {error ? (
          <Alert color="red" variant="light">
            {error}
          </Alert>
        ) : (
          <pre className="trustLogText">
            {loading ? "Loading..." : log || "No data"}
          </pre>
        )}
      </Paper>
    </UtilityPage>
  );
}

function UtilityPage({
  title,
  actions,
  children,
}: {
  title: string;
  actions?: ReactNode;
  children: ReactNode;
}) {
  return (
    <Container fluid p="sm" className="utilityPage">
      <Stack gap="sm" className="utilityLayout">
        <Group justify="space-between" gap="sm" className="utilityToolbar">
          <Group gap="xs">
            <FileText size={18} />
            <Title order={1} size="h3">
              {title}
            </Title>
          </Group>
          {actions}
        </Group>
        <div className="utilityContent">{children}</div>
      </Stack>
    </Container>
  );
}

function Panel({
  title,
  icon,
  children,
}: {
  title: string;
  icon: ReactNode;
  children: ReactNode;
}) {
  return (
    <Paper withBorder p="md" radius="md">
      <PanelHeader icon={icon} title={title} />
      <Stack gap="sm">{children}</Stack>
    </Paper>
  );
}

function PanelHeader({ icon, title }: { icon: ReactNode; title: string }) {
  return (
    <Group gap="xs" mb="sm">
      {icon}
      <Title order={2} size="h4">
        {title}
      </Title>
    </Group>
  );
}

function Metric({
  icon,
  label,
  value,
  color,
}: {
  icon: ReactNode;
  label: string;
  value: string;
  color: string;
}) {
  return (
    <Paper withBorder p="md" radius="md">
      <Group gap="sm" wrap="nowrap">
        <ThemeIcon variant="light" color={color}>
          {icon}
        </ThemeIcon>
        <div>
          <Text size="xs" c="dimmed">
            {label}
          </Text>
          <Text fw={700} style={{ overflowWrap: "anywhere" }}>
            {value}
          </Text>
        </div>
      </Group>
    </Paper>
  );
}

function Action({
  icon,
  label,
  description,
  busy,
  onClick,
}: {
  icon: ReactNode;
  label: string;
  description: string;
  busy: string | null;
  onClick: () => void;
}) {
  return (
    <Tooltip label={description} openDelay={300}>
      <Button
        variant="default"
        leftSection={icon}
        onClick={onClick}
        disabled={busy !== null}
      >
        {label}
      </Button>
    </Tooltip>
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
  if (processes.length === 0) {
    return (
      <Text size="sm" c="dimmed">
        No managed processes
      </Text>
    );
  }
  return (
    <Table striped highlightOnHover withTableBorder>
      <Table.Tbody>
        {processes.map((process) => (
          <Table.Tr key={process.name}>
            <Table.Td>
              <Text fw={600} size="sm">
                {processLabel(process.name)}
              </Text>
              <Text size="xs" c="dimmed">
                {processDescription(process.name)}
              </Text>
            </Table.Td>
            <Table.Td>
              <Badge color={process.running ? "teal" : "gray"} variant="light">
                {process.running
                  ? "running"
                  : process.exitCode === 0
                    ? "finished"
                    : "stopped"}
              </Badge>
            </Table.Td>
            <Table.Td>{process.pid ?? process.exitCode ?? ""}</Table.Td>
          </Table.Tr>
        ))}
      </Table.Tbody>
    </Table>
  );
}

function processLabel(name: string) {
  return (
    {
      dmonitor: "接続中の通信",
      rpt_conn: "待受開始処理",
      repeater_mon: "レピーター状態監視",
      repeater_mon_light: "レピーター状態監視",
      repeater_scan: "レピーター探索",
    }[name] ?? name
  );
}

function processDescription(name: string) {
  return (
    {
      dmonitor: "指定レピーターへの接続を維持",
      rpt_conn: "待受起動時に短時間だけ実行",
      repeater_mon: "使用中レピーターと一覧を更新",
      repeater_mon_light: "軽量な状態監視",
      repeater_scan: "接続可能なレピーターを探索",
    }[name] ?? "内部プロセス"
  );
}

function RepeaterCard({
  repeater,
  selected,
  onClick,
}: {
  repeater: Repeater;
  selected: boolean;
  onClick: () => void;
}) {
  return (
    <Card
      withBorder
      padding="xs"
      radius="sm"
      className="repeaterCard"
      component="button"
      onClick={onClick}
      style={{
        textAlign: "left",
        borderColor: selected ? "var(--mantine-color-teal-6)" : undefined,
        background: selected ? "var(--mantine-color-teal-0)" : undefined,
      }}
    >
      <Group justify="space-between" gap={4} wrap="nowrap">
        <Text fw={700} size="xs" truncate>
          {repeater.areaCallsign || "unknown"}
        </Text>
        <Badge
          color={repeater.active ? "teal" : statusColor(repeater.status)}
          variant="light"
          size="xs"
        >
          {repeater.active ? "active" : repeater.status || "idle"}
        </Badge>
      </Group>
      <Text size="xs" fw={600} truncate mt={2}>
        {displayRepeaterName(repeater)}
      </Text>
      <div className="repeaterMeta">
        <Text
          size="xs"
          c="dimmed"
          component="span"
          className="repeaterAddress"
          title={repeater.address || "no address"}
        >
          {repeater.address || "no address"}
        </Text>
        <Text size="xs" c="dimmed" component="span" className="repeaterPort">
          {repeater.port || "51000"}
        </Text>
      </div>
    </Card>
  );
}

function groupRepeatersByArea(repeaters: Repeater[]) {
  const groups = new Map<string, Repeater[]>();
  for (const repeater of repeaters) {
    const area =
      repeater.area || areaFromCallsign(repeater.areaCallsign) || "?";
    groups.set(area, [...(groups.get(area) ?? []), repeater]);
  }
  return [...groups.entries()]
    .sort(([a], [b]) => areaSortValue(a) - areaSortValue(b))
    .map(([area, grouped]) => ({ area, repeaters: grouped }));
}

function areaFromCallsign(callsign: string) {
  return callsign.replace(/\s+/g, "").match(/\d/)?.[0] ?? "";
}

function areaSortValue(area: string) {
  if (area === "?") return 99;
  if (area === "0") return 10;
  return Number(area);
}

function areaLabel(area: string) {
  return area === "?" ? "Unknown" : `${area} area`;
}

function displayRepeaterName(repeater: Repeater) {
  const name = (repeater.name ?? "").trim();
  if (!name || name === repeater.areaCallsign) return "no name";
  return name;
}

function statusColor(status?: string) {
  if (status === "on") return "blue";
  if (status === "off") return "gray";
  return "gray";
}

function normalizePath(path: string) {
  return path.length > 1 ? path.replace(/\/+$/, "") : path;
}

function openStandalonePage(path: string) {
  window.open(path, "_blank", "noopener,noreferrer,width=1100,height=760");
}
