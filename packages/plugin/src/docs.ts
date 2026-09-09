/**
 * Types + helpers for authoring Krate docs themes.
 *
 * A docs theme is a layout component that owns the whole docs shell — navbar,
 * sidebar, TOC, breadcrumbs, prev/next, social links (the "chrome"). The docs
 * plugin resolves it via the `theme` option in krate.config.ts and renders
 * every generated docs page through it, passing a single typed
 * {@link DocsLayoutProps} object. Helper functions are compile-time no-ops —
 * they are erased when the theme is bundled into Krate's embedded runtime.
 */

/** Options a docs theme factory accepts. Theme authors narrow this generic. */
export type DocsThemeOptions = Record<string, unknown>;

/** A single entry in the docs sidebar tree. */
export interface DocsSidebarItem {
  /** Display text (section header or link label). */
  title?: string;
  /** Clickable link (empty/omitted = plain section header). */
  url?: string;
  /** Highlighted when the current page's path matches (computed by krate). */
  active?: boolean;
  /** Nested folder section. */
  children?: DocsSidebarItem[];
  /** Auto-filled by krate for collapsible section home pages. */
  indexURL?: string;
  /** Whether the section shows a collapse toggle. */
  collapsible?: boolean;
  /** Whether the section is expanded for the current page. */
  expanded?: boolean;
}

/** Search configuration forwarded to the theme via the layout props. */
export interface DocsSearchOptions {
  /** Search bar on/off (default: true). */
  enabled?: boolean;
  /** Index backend: "docfind" (default) or classic "json". */
  engine?: "docfind" | "json";
  /** Max number of results shown (default: 8). */
  maxResults?: number;
}

/** Every prop a generated docs page passes to the theme's layout component. */
export interface DocsLayoutProps<Options = DocsThemeOptions> {
  /** Current page's title (from frontmatter or the filename). */
  pageTitle: string;
  /** Site title set via `docs({ title })`. */
  siteTitle: string;
  /** Recursive sidebar tree (enriched: active/expanded/collapsible filled in). */
  sidebarItems: DocsSidebarItem[];
  /** Table-of-contents headings extracted from the rendered page. */
  tocItems: { title: string; id: string; depth: number }[];
  /** Breadcrumb trail for the current page. */
  breadcrumbs: { label: string; url: string; isLast: boolean }[];
  /** Previous page title/link in sidebar order. */
  prevTitle?: string;
  prevLink?: string;
  /** Next page title/link in sidebar order. */
  nextTitle?: string;
  nextLink?: string;
  /** `docs({ links })` social links. */
  socialLinks: { icon?: string; url?: string }[];
  /** Current page path (e.g. "getting-started"). */
  currentPath: string;
  /** Rendered markdown content. */
  children?: unknown;
  /** Theme options forwarded when the descriptor declares them. */
  options?: Options;
}

/**
 * What a docs theme factory returns (the `theme` option value in
 * krate.config.ts). The generator imports `module` (or `layout`) and renders
 * every docs page through it. `module` is filled automatically by the factory
 * via `import.meta.url`, mirroring plugin factories.
 */
export interface DocsThemeDescriptor<Options = DocsThemeOptions> {
  /** Optional theme name for diagnostics. */
  name?: string;
  /**
   * Path (or file:// URL) to the theme's layout component. Krate bundles it
   * and renders every docs page through it. Filled by the factory.
   */
  module?: string;
  /** Alias of `module` — a root-relative component path, like `layout`. */
  layout?: string;
  /** Options forwarded to the layout component as `props.options`. */
  options?: Options;
}

/**
 * Identity helper that type-checks a docs theme factory's return descriptor.
 * Use it inside a theme module to build the default export factory:
 *
 * @example
 * export default function nightTheme(options: NightThemeOptions = {}) {
 *   return defineDocsTheme({
 *     name: "night-theme",
 *     module: import.meta.url,
 *     options,
 *   });
 * }
 */
export function defineDocsTheme<Options = DocsThemeOptions>(
  descriptor: DocsThemeDescriptor<Options>,
): DocsThemeDescriptor<Options> {
  return descriptor;
}