import i18n from "i18next";
import { derived, writable, type Readable } from "svelte/store";
import type { TFunction } from "i18next";
import { en, zh, type AppResources } from "./resources.js";

declare module "i18next" {
  interface CustomTypeOptions {
    defaultNS: "translation";
    resources: AppResources;
  }
}

const LANG_KEY = "hidane-lang";

export const language = writable<"zh" | "en">("zh");

/** A reactive translation function for Svelte templates. */
export const t: Readable<TFunction> = derived(language, () => i18n.t.bind(i18n) as TFunction);

export function storedLanguage(): "zh" | "en" {
  const v = typeof localStorage !== "undefined" ? localStorage.getItem(LANG_KEY) : null;
  return v === "en" ? "en" : "zh";
}

export function switchLanguage(lng: "zh" | "en"): void {
  if (typeof localStorage !== "undefined") localStorage.setItem(LANG_KEY, lng);
  void i18n.changeLanguage(lng);
  language.set(lng);
  if (typeof document !== "undefined") {
    document.documentElement.lang = lng === "zh" ? "zh-CN" : "en";
  }
}

void i18n.init({
  resources: { zh, en },
  lng: storedLanguage(),
  fallbackLng: "zh",
  interpolation: { escapeValue: false },
});

language.set(storedLanguage());
i18n.on("languageChanged", (lng) => {
  if (lng === "zh" || lng === "en") language.set(lng);
  if (typeof document !== "undefined") {
    document.documentElement.lang = lng === "zh" ? "zh-CN" : "en";
  }
});

export default i18n;
