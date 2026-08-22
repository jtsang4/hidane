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

export function today(): string {
  const d = new Date();
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
}
