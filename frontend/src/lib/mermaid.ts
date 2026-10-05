import DOMPurify from "dompurify";
import type { Mermaid, MermaidConfig } from "mermaid";

/** Mermaid is large: it loads with the first diagram, not with the app. */
let loading: Promise<Mermaid> | undefined;
let rendered = 0;

/** A fenced block marked ```mermaid. */
export function isMermaidBlock(pre: HTMLPreElement): boolean {
  return pre.querySelector("code")?.classList.contains("language-mermaid") ?? false;
}

/** How far a wide diagram shrinks to fit (13px labels down to 11px) before it scrolls sideways instead, as a wide code block does. */
const MIN_SCALE = 0.85;

/**
 * The diagram, sanitized, or null when Mermaid cannot draw the source (it then
 * stays a code block). Agent text is untrusted: Mermaid runs at its strict
 * level, and its output is sanitized again here rather than trusting that
 * option name alone.
 */
export async function renderMermaid(source: string): Promise<DocumentFragment | null> {
  const mermaid = await (loading ??= load());
  if (!(await mermaid.parse(source, { suppressErrors: true }))) return null;
  const id = `mermaid-${++rendered}`;
  let svg: string;
  try {
    ({ svg } = await mermaid.render(id, source));
  } finally {
    // What a failed render leaves behind in <body>.
    document.getElementById(`d${id}`)?.remove();
  }
  const diagram = DOMPurify.sanitize(svg, {
    RETURN_DOM_FRAGMENT: true,
    USE_PROFILES: { svg: true, svgFilters: true, html: true },
    // Labels are HTML inside <foreignObject>.
    ADD_TAGS: ["foreignObject"],
    HTML_INTEGRATION_POINTS: { foreignobject: true },
  });
  const root = diagram.querySelector("svg");
  const natural = Number.parseFloat(root?.style.maxWidth ?? "");
  // A Gantt chart has no width of its own: it spreads across the page it was drawn on.
  if (root && natural > 0 && root.getAttribute("aria-roledescription") !== "gantt") {
    root.style.minWidth = `${Math.round(natural * MIN_SCALE)}px`;
  }
  return diagram;
}

async function load(): Promise<Mermaid> {
  try {
    const { default: mermaid } = await import("mermaid");
    mermaid.initialize(config());
    return mermaid;
  } catch (error) {
    loading = undefined;
    throw error;
  }
}

/** Where a diagram is drawn: a well (like a code block) inside an agent's bubble. */
const GROUND = ["surface-2", "well"];

/**
 * Mermaid works its palette out with khroma, which reads hex but not the
 * oklch tokens: each token becomes the color it paints over the diagram's
 * ground, translucent ones included.
 */
function painter(): (token: string, alpha?: number) => string {
  const style = getComputedStyle(document.documentElement);
  const canvas = document.createElement("canvas");
  canvas.width = canvas.height = 1;
  const context = canvas.getContext("2d", { willReadFrequently: true });
  const value = (token: string) => style.getPropertyValue(`--color-${token}`).trim();
  return (token, alpha = 1) => {
    if (!context) return value(token);
    context.globalAlpha = 1;
    for (const layer of GROUND) {
      context.fillStyle = value(layer);
      context.fillRect(0, 0, 1, 1);
    }
    context.globalAlpha = alpha;
    context.fillStyle = value(token);
    context.fillRect(0, 0, 1, 1);
    const [r = 0, g = 0, b = 0] = context.getImageData(0, 0, 1, 1).data;
    return `#${[r, g, b].map((c) => c.toString(16).padStart(2, "0")).join("")}`;
  };
}

/**
 * The design system's dark layers in Mermaid's terms: shapes are raised
 * surfaces with hairline edges, lines and arrows are muted, and the ember
 * appears only for live state (a Gantt chart's active work and today).
 * Categories (pie slices, chart series, mindmap branches) are steps of light,
 * never hues.
 */
function config(): MermaidConfig {
  const paint = painter();
  const fontFamily = getComputedStyle(document.documentElement).getPropertyValue("--font-sans").trim();
  const ground = paint("well", 0);
  const shape = paint("surface-2");
  const raised = paint("accent-strong");
  const sunk = paint("surface");
  const edge = paint("input-strong");
  const hairline = paint("border");
  const line = paint("muted");
  const text = paint("foreground");
  const steps = [0.42, 0.33, 0.26, 0.2, 0.15, 0.11, 0.38, 0.29, 0.23, 0.17, 0.13, 0.09].map((alpha) => paint("foreground", alpha));
  const each = (prefix: string, value: (i: number) => string) => Object.fromEntries(steps.map((_, i) => [`${prefix}${i}`, value(i)]));

  return {
    startOnLoad: false,
    securityLevel: "strict",
    theme: "base",
    look: "classic",
    fontFamily,
    fontSize: 13,
    // The theme has no radius; shapes take the system's, as rows and cards do.
    themeCSS: `
      .node rect.label-container, rect.actor, rect.note, rect.activation0, rect.activation1, rect.activation2 { rx: var(--radius-sm); ry: var(--radius-sm); }
      .cluster rect { rx: var(--radius-md); ry: var(--radius-md); }
    `,
    flowchart: { padding: 12, nodeSpacing: 32, rankSpacing: 40, diagramPadding: 4 },
    sequence: { diagramMarginX: 4, diagramMarginY: 4, height: 40 },
    themeVariables: {
      darkMode: true,
      fontFamily,
      fontSize: "13px",
      useGradient: false,
      dropShadow: "none",
      background: ground,
      primaryColor: shape,
      primaryTextColor: text,
      primaryBorderColor: edge,
      secondaryColor: raised,
      secondaryTextColor: text,
      secondaryBorderColor: edge,
      tertiaryColor: sunk,
      tertiaryTextColor: text,
      tertiaryBorderColor: hairline,
      mainBkg: shape,
      nodeBorder: edge,
      clusterBkg: sunk,
      clusterBorder: hairline,
      lineColor: line,
      textColor: text,
      titleColor: text,
      edgeLabelBackground: ground,
      noteBkgColor: raised,
      noteTextColor: text,
      noteBorderColor: edge,
      actorBkg: shape,
      actorBorder: edge,
      actorTextColor: text,
      actorLineColor: paint("input"),
      signalColor: line,
      signalTextColor: text,
      labelBoxBkgColor: shape,
      labelBoxBorderColor: edge,
      labelTextColor: text,
      loopTextColor: text,
      activationBkgColor: raised,
      activationBorderColor: edge,
      sequenceNumberColor: ground,
      rowOdd: shape,
      rowEven: sunk,
      attributeBackgroundColorOdd: shape,
      attributeBackgroundColorEven: sunk,
      taskBkgColor: shape,
      taskBorderColor: edge,
      taskTextColor: text,
      taskTextLightColor: text,
      taskTextDarkColor: text,
      taskTextOutsideColor: text,
      activeTaskBkgColor: paint("primary", 0.22),
      activeTaskBorderColor: paint("primary"),
      doneTaskBkgColor: sunk,
      doneTaskBorderColor: hairline,
      critBkgColor: paint("danger", 0.22),
      critBorderColor: paint("danger"),
      todayLineColor: paint("primary"),
      gridColor: hairline,
      sectionBkgColor: paint("accent"),
      sectionBkgColor2: sunk,
      altSectionBkgColor: ground,
      excludeBkgColor: sunk,
      errorBkgColor: paint("danger", 0.22),
      errorTextColor: text,
      ...each("cScale", (i) => steps[i]!),
      ...each("cScaleLabel", () => text),
      ...each("cScaleInv", (i) => steps[i]!),
      ...Object.fromEntries(steps.map((step, i) => [`pie${i + 1}`, step])),
      pieOpacity: "1",
      pieStrokeColor: ground,
      pieStrokeWidth: "1.5px",
      pieOuterStrokeColor: hairline,
      pieOuterStrokeWidth: "1px",
      pieTitleTextSize: "14px",
      pieTitleTextColor: text,
      pieSectionTextSize: "12px",
      pieSectionTextColor: text,
      pieLegendTextSize: "12px",
      pieLegendTextColor: text,
      xyChart: { plotColorPalette: steps.slice(0, 6).join(",") },
      radar: { graticuleColor: edge },
    },
  };
}
