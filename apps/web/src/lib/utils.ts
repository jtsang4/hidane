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

/** Step a YYYY-MM-DD day, staying in local time so DST cannot shift the date. */
export function shiftDay(day: string, delta: number): string {
  const [y, m, d] = day.split("-").map(Number);
  if (!y || !m || !d) return day;
  return ymd(new Date(y, m - 1, d + delta));
}
