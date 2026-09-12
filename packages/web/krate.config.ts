import { defineConfig, docs, sitemap } from '@krate/core';
import { baseDocsTheme } from '@krate/base-docs-theme';

export default defineConfig({
  entry: "src/pages/index.tsx",
  outDir: "dist",
  pagesDir: "src/pages",
  publicDir: "public",
  minify: true,

  devServer: {
    port: 3000,
    open: false,
  },

  markdown: {
    gfm: true,
    headingAnchors: true,
    admonitions: true,
    codeHighlight: true,
  },

  seo: {
    baseUrl: "https://krate.js.org",
    siteName: "Krate",
    description: "A Go-native static site generator with signal-based reactivity.",
  },

  plugins: [
    sitemap({ baseUrl: "https://krate.js.org", changeFreq: "daily", priority: "0.8" }),
    docs({
      contentDir: "src/content/docs",
      title: "Krate Docs",
      theme: baseDocsTheme(),
      editLinkBase: "https://github.com/kratejs/krate/blob/main/packages/web",
      search: {
        enabled: true,
        engine: "docfind",
        maxResults: 8,
      },
      links: [
        { icon: "lucide:github", url: "https://github.com/kratejs/krate" },
      ],
    }),
  ],
});
