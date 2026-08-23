import { clsx, type ClassValue } from "clsx";
import { twMerge } from "tailwind-merge";
import i18n from "../i18n/index.js";

export function cn(...inputs: ClassValue[]): string {
  return twMerge(clsx(inputs));
}

function dateLocale(): string {
  return i18n.language === "en" ? "en-US" : "zh-CN";
}

export function fmtTime(iso: string): string {
  return new Date(iso).toLocaleTimeString(dateLocale(), { hour12: false });
}

export function fmtDateTime(iso: string): string {
  const d = new Date(iso);
  return `${d.toLocaleDateString(dateLocale())} ${fmtTime(iso)}`;
}

function pad(n: number): string {
  return String(n).padStart(2, "0");
}

function ymd(d: Date): string {
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
}

export function today(): string {
  return ymd(new Date());
}

/**
 * "3 分钟前" rather than a wall-clock time.
 *
 * A live feed is read as "how long ago", and turning 14:32:07 into that is
 * arithmetic the reader should not have to do. Precision is not lost: callers
 * keep the absolute time in a title attribute.
 *
 * Beyond a week the relative form stops helping ("13 天前" is worse than a
 * date), so it falls back to the absolute one.
 */
export function fmtRelative(iso: string, now = Date.now()): string {
  const then = new Date(iso).getTime();
  if (!Number.isFinite(then)) return iso;
  const seconds = Math.round((now - then) / 1000);
  const t = i18n.t.bind(i18n);

  // Clock skew between server and browser can make a fresh event look future.
  if (seconds < 45) return t("time.justNow");
  if (seconds < 90) return t("time.minute", { n: 1 });
  const minutes = Math.round(seconds / 60);
  if (minutes < 60) return t("time.minute", { n: minutes });
  const hours = Math.round(minutes / 60);
  if (hours < 24) return t("time.hour", { n: hours });
  const days = Math.round(hours / 24);
  if (days === 1) return t("time.yesterday", { time: fmtTime(iso) });
  if (days < 7) return t("time.day", { n: days });
  return fmtDateTime(iso);
}

/** Step a YYYY-MM-DD day, staying in local time so DST cannot shift the date. */
export function shiftDay(day: string, delta: number): string {
  const [y, m, d] = day.split("-").map(Number);
  if (!y || !m || !d) return day;
  return ymd(new Date(y, m - 1, d + delta));
}
