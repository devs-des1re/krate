import { defineDocsTheme } from "@krate/plugin";

/**
 * Options for the base docs theme. Reserved for forward compatibility — the
 * layout currently renders the stock light/dark theme that Krate ships in the
 * examples and docs sites.
 */
export interface BaseDocsThemeOptions {
  [key: string]: unknown;
}

/**
 * The default Krate docs theme. Pass the factory result to the docs plugin:
 *
 * ```ts
 * import { docs } from "@krate/core";
 * import { baseDocsTheme } from "@krate/base-docs-theme";
 *
 * docs({
 *   contentDir: "content/docs",
 *   theme: baseDocsTheme(),
 * })
 * ```
 */
export function baseDocsTheme(options: BaseDocsThemeOptions = {}) {
  return defineDocsTheme({
    name: "@krate/base-docs-theme",
    module: new URL("./layout.tsx", import.meta.url).href,
    options,
  });
}

export default baseDocsTheme;