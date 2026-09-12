import { defineConfig, defineContent, docs, sitemap } from '@krate/core';
import demoPlugin from './plugins/krate-plugin-demo';
import { baseDocsTheme } from '@krate/base-docs-theme';

export default defineConfig({
  entry: "src/pages/index.tsx",
  outDir: "dist",
  pagesDir: "src/pages",
  publicDir: "public",
  minify: true,
  sourcemap: true,
  emitReact: true,
  devServer: {
    port: 3001,
    open: false,
  },
  markdown: {
    gfm: true,
    headingAnchors: true,
    admonitions: true,
    codeHighlight: true,
    codeTheme: "github-dark",
  },
  tailwind: {
    enabled: true,
  },
  plugins: [
    sitemap({ baseUrl: "https://example.com" }),
    docs({
      contentDir: "content/docs",
      title: "Krate Docs",
      theme: baseDocsTheme(),
      search: {
        enabled: true,
        engine: "docfind",
        maxResults: 8,
      },
      links: [
        { icon: "lucide:github", url: "https://github.com/kratejs" },
        { icon: "lucide:twitter", url: "https://twitter.com/kratejs" }
      ],
    }),
    demoPlugin({ greeting: "Hello from the Krate Demo Plugin!" }),
  ],
  seo: {
    baseUrl: "https://example.com",
    siteName: "Krate Test",
    description: "A modern static site generator",
  },
  robots: {
    allow: "/",
  },
  redirects: [
    { source: "/old-about", destination: "/about", permanent: true },
    { source: "/legacy/*", destination: "/docs/:splat", permanent: false },
  ],
  rewrites: [
    { source: "/help/:path*", destination: "/about" },
  ],
  // Typed content collections: entries under src/content/blog are validated
  // against this schema and typed in .krate/types/content.d.ts.
  content: defineContent({
    blog: {
      dir: 'src/content/blog',
      schema: {
        title: 'string',
        description: 'string',
        date: 'date',
        author: 'string',
        tags: 'string[]',
        draft: 'boolean',
        order: { type: 'number', required: true },
      },
    },
  }),
});
