import { mount } from "svelte";
import App from "./App.svelte";
import "./styles.css";
import "./i18n/index.js";
import { consumeUrlToken } from "./lib/urlToken.js";
import { boot } from "./lib/boot.js";
import { installDesktopLinks } from "./lib/desktopLinks.js";

const target = document.getElementById("root");
if (!target) throw new Error("Missing #root element");

consumeUrlToken();
if (boot().desktop) installDesktopLinks();
mount(App, { target });
