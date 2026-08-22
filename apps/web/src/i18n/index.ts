import i18n from "i18next";
import { initReactI18next } from "react-i18next";
import { en, zh, type AppResources } from "./resources.js";

declare module "i18next" {
  interface CustomTypeOptions {
    defaultNS: "translation";
    resources: AppResources;
  }
}

const LANG_KEY = "hidane-lang";

export function storedLanguage(): "zh" | "en" {
  const v = typeof localStorage !== "undefined" ? localStorage.getItem(LANG_KEY) : null;
  return v === "en" ? "en" : "zh";
}

export function switchLanguage(lng: "zh" | "en"): void {
  localStorage.setItem(LANG_KEY, lng);
  void i18n.changeLanguage(lng);
  document.documentElement.lang = lng === "zh" ? "zh-CN" : "en";
}

void i18n.use(initReactI18next).init({
  resources: { zh, en },
  lng: storedLanguage(),
  fallbackLng: "zh",
  interpolation: { escapeValue: false },
});

export default i18n;
