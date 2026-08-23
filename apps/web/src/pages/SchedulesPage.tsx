import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Play, Plus, Trash2 } from "lucide-react";
import { useTranslation } from "react-i18next";
import { api, ApiError, type Schedule, type ScheduleInput } from "../lib/api.js";
import { pushToast } from "../lib/toast.js";
import { cn, fmtDateTime } from "../lib/utils.js";
import { Badge, Button, Card, Input, Textarea } from "../components/ui/primitives.js";

function TimingBadge({ schedule }: { schedule: Schedule }) {
  const { t } = useTranslation();
  const label = schedule.cron
    ? `cron ${schedule.cron}${schedule.timezone ? ` (${schedule.timezone})` : ""}`
    : t("schedules.everySec", { s: schedule.intervalSec ?? 0 });
  return <Badge tone="muted">{label}</Badge>;
}

function errText(err: unknown): string {
  return err instanceof ApiError ? err.message : String(err);
}

function ScheduleCard({ schedule }: { schedule: Schedule }) {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const invalidate = () => void queryClient.invalidateQueries({ queryKey: ["schedules"] });

  const toggle = useMutation({
    mutationFn: () => api.updateSchedule(schedule.id, { enabled: !schedule.enabled }),
    onSuccess: invalidate,
    onError: (err) => pushToast(errText(err)),
  });
  const run = useMutation({
    mutationFn: () => api.runSchedule(schedule.id),
    onSuccess: (res) => {
      pushToast(t("schedules.ranNow", { status: res.status }), "default");
      invalidate();
    },
    onError: (err) => pushToast(errText(err)),
  });
  const remove = useMutation({
    mutationFn: () => api.deleteSchedule(schedule.id),
    onSuccess: invalidate,
    onError: (err) => pushToast(errText(err)),
  });

  const detail =
    schedule.action === "http"
      ? `${schedule.spec.method ?? "GET"} ${schedule.spec.url ?? ""}${schedule.spec.wake ? ` · ${t("schedules.wakes")}` : ""}`
      : schedule.spec.prompt ?? "";

  return (
    <Card className={cn("space-y-2", !schedule.enabled && "opacity-60")}>
      <div className="flex flex-wrap items-center gap-2">
        <span className="font-medium">{schedule.name}</span>
        <Badge tone={schedule.action === "http" ? "default" : "success"}>
          {t(`schedules.action.${schedule.action}`)}
        </Badge>
        <TimingBadge schedule={schedule} />
        <div className="ml-auto flex items-center gap-1">
          <Button
            variant="ghost"
            size="icon"
            aria-label={`${t("schedules.runNow")} ${schedule.name}`}
            disabled={run.isPending}
            onClick={() => run.mutate()}
          >
            <Play className="h-4 w-4" />
          </Button>
          <Button
            variant="ghost"
            size="icon"
            aria-label={`${t("schedules.delete")} ${schedule.name}`}
            disabled={remove.isPending}
            onClick={() => {
              if (confirm(t("schedules.confirmDelete", { name: schedule.name }))) remove.mutate();
            }}
          >
            <Trash2 className="h-4 w-4 text-danger" />
          </Button>
          <Button
            variant="outline"
            size="sm"
            disabled={toggle.isPending}
            onClick={() => toggle.mutate()}
          >
            {schedule.enabled ? t("schedules.disable") : t("schedules.enable")}
          </Button>
        </div>
      </div>
      <p className="text-xs break-all whitespace-pre-wrap text-muted">{detail}</p>
      <p className="text-xs text-muted">
        {schedule.enabled && schedule.nextRunAt
          ? t("schedules.nextRun", { time: fmtDateTime(schedule.nextRunAt) })
          : t("schedules.paused")}
        {schedule.lastRunAt &&
          ` · ${t("schedules.lastRun", { time: fmtDateTime(schedule.lastRunAt), status: schedule.lastStatus ?? "" })}`}
      </p>
    </Card>
  );
}

const EMPTY_FORM = {
  name: "",
  action: "prompt" as "prompt" | "http",
  timing: "interval" as "interval" | "cron",
  intervalSec: "3600",
  cron: "",
  timezone: "",
  prompt: "",
  url: "",
  wake: false,
};

function CreateForm({ onDone }: { onDone: () => void }) {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const [form, setForm] = useState(EMPTY_FORM);
  const set = <K extends keyof typeof EMPTY_FORM>(key: K, value: (typeof EMPTY_FORM)[K]) =>
    setForm((f) => ({ ...f, [key]: value }));

  const create = useMutation({
    mutationFn: (input: ScheduleInput) => api.createSchedule(input),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["schedules"] });
      setForm(EMPTY_FORM);
      onDone();
    },
    onError: (err) => pushToast(errText(err)),
  });

  const submit = () => {
    const input: ScheduleInput = {
      name: form.name,
      action: form.action,
      spec:
        form.action === "prompt"
          ? { prompt: form.prompt }
          : { url: form.url, wake: form.wake },
      ...(form.timing === "cron"
        ? { cron: form.cron, ...(form.timezone ? { timezone: form.timezone } : {}) }
        : { intervalSec: Number(form.intervalSec) }),
    };
    create.mutate(input);
  };

  const Mode = ({
    active,
    label,
    onClick,
  }: {
    active: boolean;
    label: string;
    onClick: () => void;
  }) => (
    <Button variant={active ? "default" : "outline"} size="sm" onClick={onClick}>
      {label}
    </Button>
  );

  return (
    <Card className="space-y-3">
      <Input
        placeholder={t("schedules.form.name")}
        value={form.name}
        onChange={(e) => set("name", e.target.value)}
      />
      <div className="flex flex-wrap items-center gap-2">
        <span className="text-xs text-muted">{t("schedules.form.actionLabel")}</span>
        <Mode
          active={form.action === "prompt"}
          label={t("schedules.action.prompt")}
          onClick={() => set("action", "prompt")}
        />
        <Mode
          active={form.action === "http"}
          label={t("schedules.action.http")}
          onClick={() => set("action", "http")}
        />
        <span className="ml-3 text-xs text-muted">{t("schedules.form.timingLabel")}</span>
        <Mode
          active={form.timing === "interval"}
          label={t("schedules.form.interval")}
          onClick={() => set("timing", "interval")}
        />
        <Mode
          active={form.timing === "cron"}
          label="cron"
          onClick={() => set("timing", "cron")}
        />
      </div>
      {form.timing === "interval" ? (
        <Input
          type="number"
          min={10}
          placeholder={t("schedules.form.intervalSec")}
          value={form.intervalSec}
          onChange={(e) => set("intervalSec", e.target.value)}
        />
      ) : (
        <div className="flex gap-2">
          <Input
            placeholder={t("schedules.form.cron")}
            value={form.cron}
            onChange={(e) => set("cron", e.target.value)}
          />
          <Input
            className="w-44"
            placeholder={t("schedules.form.timezone")}
            value={form.timezone}
            onChange={(e) => set("timezone", e.target.value)}
          />
        </div>
      )}
      {form.action === "prompt" ? (
        <Textarea
          rows={3}
          placeholder={t("schedules.form.prompt")}
          value={form.prompt}
          onChange={(e) => set("prompt", e.target.value)}
        />
      ) : (
        <div className="space-y-2">
          <Input
            placeholder={t("schedules.form.url")}
            value={form.url}
            onChange={(e) => set("url", e.target.value)}
          />
          <label className="flex items-center gap-2 text-sm text-muted">
            <input
              type="checkbox"
              checked={form.wake}
              onChange={(e) => set("wake", e.target.checked)}
            />
            {t("schedules.form.wake")}
          </label>
        </div>
      )}
      <div className="flex justify-end gap-2">
        <Button variant="outline" size="sm" onClick={onDone}>
          {t("common.cancel")}
        </Button>
        <Button size="sm" disabled={create.isPending} onClick={submit}>
          {t("schedules.form.create")}
        </Button>
      </div>
    </Card>
  );
}

/**
 * The active side of the connector surface: user-defined timers. Web/Feishu/
 * webhook wait for something to arrive — these make the runtime act on its
 * own: poll a URL on a period, or hand the agent a task on a clock.
 */
export function SchedulesPage() {
  const { t } = useTranslation();
  const [creating, setCreating] = useState(false);
  const { data, isLoading } = useQuery({
    queryKey: ["schedules"],
    queryFn: () => api.schedules(),
  });
  const schedules = data?.schedules ?? [];

  return (
    <div className="space-y-3 p-4">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-lg font-semibold">{t("schedules.title")}</h1>
          <p className="mt-1 text-xs text-muted">{t("schedules.subtitle")}</p>
        </div>
        {!creating && (
          <Button size="sm" onClick={() => setCreating(true)}>
            <Plus className="h-4 w-4" /> {t("schedules.new")}
          </Button>
        )}
      </div>
      {creating && <CreateForm onDone={() => setCreating(false)} />}
      {isLoading && <p className="text-sm text-muted">{t("common.loading")}</p>}
      {!isLoading && schedules.length === 0 && !creating && (
        <p className="pt-8 text-center text-sm text-muted">{t("schedules.empty")}</p>
      )}
      {schedules.map((s) => (
        <ScheduleCard key={s.id} schedule={s} />
      ))}
    </div>
  );
}
