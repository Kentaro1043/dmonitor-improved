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
  const activeRepeaters = status?.runtime.activeRepeaters ?? [];
  const processes = status?.runtime.processes ?? {};
  const processRows = Object.values(processes);

  async function refresh() {
    const [nextStatus, nextLogs] = await Promise.all([
      api.status(),
      api.logs(),
    ]);
    setStatus(nextStatus);
    setConfig(nextStatus.config);
    setLogs((nextLogs.logs ?? []).slice(-80).reverse());
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
    () => processRows.filter((process) => process.running).length,
    [processRows],
  );
  const repeaterGroups = useMemo(
    () => groupRepeatersByArea(repeaters),
    [repeaters],
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

  const metrics = [
    {
      icon: <Radio size={18} />,
      label: "Device",
      value: status?.device.dstarExists
        ? "/dev/dstar"
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
      label: "rpt_conn",
      action: () => api.post("/api/runtime/start-rpt-conn"),
    },
    {
      id: "stop-rpt",
      icon: <CircleStop size={16} />,
      label: "stop rpt",
      action: () => api.post("/api/runtime/stop-rpt-conn"),
    },
    {
      id: "scan",
      icon: <Search size={16} />,
      label: "scan",
      action: () => api.post("/api/repeater/scan/start"),
    },
    {
      id: "stop-scan",
      icon: <CircleStop size={16} />,
      label: "stop scan",
      action: () => api.post("/api/repeater/scan/stop"),
    },
    {
      id: "disconnect",
      icon: <Unplug size={16} />,
      label: "disconnect",
      action: () => api.post("/api/monitor/disconnect"),
    },
    {
      id: "update",
      icon: <Download size={16} />,
      label: "update list",
      action: () => api.post("/api/repeater/update"),
    },
    {
      id: "buffer-plus",
      icon: <Plus size={16} />,
      label: "buffer",
      action: () => api.post("/api/buffer/increase"),
    },
    {
      id: "buffer-minus",
      icon: <Minus size={16} />,
      label: "buffer",
      action: () => api.post("/api/buffer/decrease"),
    },
  ];

  return (
    <Container size="xl" py="md">
      <Stack gap="md">
        <Group justify="space-between" align="flex-start">
          <div>
            <Title order={1} size="h2">
              dmonitor improved
            </Title>
            <Text size="sm" c="dimmed">
              {status?.runtime.rootfs ?? "runtime/rootfs"}
            </Text>
          </div>
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

        <SimpleGrid cols={{ base: 1, md: 3 }}>
          <Panel title="Runtime" icon={<Activity size={18} />}>
            <SimpleGrid cols={2} spacing="xs">
              {runtimeActions.map((item) => (
                <Action
                  key={item.id}
                  icon={item.icon}
                  label={item.label}
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
                onChange={(value) =>
                  setConfig({ ...config, rig: value ?? "ICOM" })
                }
              />
              <Select
                label="LCD"
                data={["NONE"]}
                value={config.lcd}
                onChange={(value) =>
                  setConfig({ ...config, lcd: value ?? "NONE" })
                }
              />
              <TextInput
                label="Callsign"
                value={config.callsign}
                maxLength={8}
                onChange={(event) =>
                  setConfig({
                    ...config,
                    callsign: event.currentTarget.value.toUpperCase(),
                  })
                }
              />
              <Switch
                label="GPS skip"
                checked={config.gpsSkipMode === "SKIP"}
                onChange={(event) =>
                  setConfig({
                    ...config,
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
        </SimpleGrid>

        <Panel title="Repeaters" icon={<Search size={18} />}>
          <Stack gap="sm">
            {activeRepeaters.length > 0 && (
              <ScrollArea type="auto" offsetScrollbars>
                <Group wrap="nowrap" gap="xs">
                  {activeRepeaters.map((repeater, idx) => (
                    <Card
                      key={`active-${repeater.areaCallsign}-${idx}`}
                      withBorder
                      padding="sm"
                      radius="md"
                      miw={180}
                      component="button"
                      onClick={() => setSelected(repeater)}
                    >
                      <Text fw={700} size="sm">
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
                defaultValue={repeaterGroups
                  .slice(0, 1)
                  .map((group) => group.area)}
              >
                {repeaterGroups.map((group) => (
                  <Accordion.Item value={group.area} key={group.area}>
                    <Accordion.Control>
                      <Group justify="space-between" pr="sm">
                        <Text fw={700}>{areaLabel(group.area)}</Text>
                        <Badge variant="light">{group.repeaters.length}</Badge>
                      </Group>
                    </Accordion.Control>
                    <Accordion.Panel>
                      <SimpleGrid cols={{ base: 1, sm: 2, lg: 3 }}>
                        {group.repeaters.map((repeater, idx) => (
                          <RepeaterCard
                            key={`${repeater.areaCallsign}-${idx}`}
                            repeater={repeater}
                            selected={selected === repeater}
                            onClick={() => setSelected(repeater)}
                          />
                        ))}
                      </SimpleGrid>
                    </Accordion.Panel>
                  </Accordion.Item>
                ))}
              </Accordion>
            )}
          </Stack>
        </Panel>

        <Panel title="Logs" icon={<Activity size={18} />}>
          <ScrollArea h={360} type="auto" offsetScrollbars>
            <Stack gap="xs">
              {logs.map((entry, idx) => (
                <Paper
                  key={`${entry.time}-${idx}`}
                  p="xs"
                  radius="sm"
                  withBorder
                >
                  <Group gap="xs" align="flex-start" wrap="nowrap">
                    <Text size="xs" c="dimmed" w={160}>
                      {entry.time.replace("T", " ").replace("Z", "")}
                    </Text>
                    <Badge variant="light" color="gray" miw={100}>
                      {entry.source}
                    </Badge>
                    <Text className="logMessage" size="xs">
                      {entry.message}
                    </Text>
                  </Group>
                </Paper>
              ))}
              {logs.length === 0 && (
                <Text size="sm" c="dimmed">
                  No logs
                </Text>
              )}
            </Stack>
          </ScrollArea>
        </Panel>
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
  busy,
  onClick,
}: {
  icon: ReactNode;
  label: string;
  busy: string | null;
  onClick: () => void;
}) {
  return (
    <Button
      variant="default"
      leftSection={icon}
      onClick={onClick}
      disabled={busy !== null}
    >
      {label}
    </Button>
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
            <Table.Td>{process.name}</Table.Td>
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
      padding="sm"
      radius="md"
      component="button"
      onClick={onClick}
      style={{
        textAlign: "left",
        borderColor: selected ? "var(--mantine-color-teal-6)" : undefined,
        background: selected ? "var(--mantine-color-teal-0)" : undefined,
      }}
    >
      <Group justify="space-between" gap="xs" wrap="nowrap">
        <Text fw={700} truncate>
          {repeater.areaCallsign || "unknown"}
        </Text>
        <Badge
          color={repeater.active ? "teal" : statusColor(repeater.status)}
          variant="light"
        >
          {repeater.active ? "active" : repeater.status || "idle"}
        </Badge>
      </Group>
      <Text size="sm" fw={600} truncate mt={4}>
        {displayRepeaterName(repeater)}
      </Text>
      <Group gap="xs" mt={6}>
        <Text size="xs" c="dimmed">
          {repeater.address || "no address"}
        </Text>
        <Text size="xs" c="dimmed">
          {repeater.port || "51000"}
        </Text>
      </Group>
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
