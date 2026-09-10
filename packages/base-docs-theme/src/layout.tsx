import { SidebarNav, TOCNav, Breadcrumbs, PrevNext, SocialLinks } from "./chrome";
import { createSignal, createEffect, onMount } from "@krate/runtime";
import "./theme.css";

interface SidebarItem {
  title: string;
  url: string;
  active?: boolean;
  indexURL?: string;
  collapsible?: boolean;
  expanded?: boolean;
  children?: SidebarItem[];
}

interface TOCItem {
  title: string;
  id: string;
  depth: number;
}

interface BreadcrumbItem {
  label: string;
  url: string;
  isLast: boolean;
}

interface SocialLinkItem {
  icon: string;
  url: string;
  name: string;
}

interface BaseDocsLayoutProps {
  pageTitle: string;
  siteTitle: string;
  children: any;
  sidebarItems: SidebarItem[];
  tocItems: TOCItem[];
  breadcrumbs: BreadcrumbItem[];
  prevTitle?: string;
  prevLink?: string;
  nextTitle?: string;
  nextLink?: string;
  socialLinks: SocialLinkItem[];
  currentPath: string;
}

export default function BaseDocsLayout(props: BaseDocsLayoutProps) {
  const pageTitle = props.pageTitle;
  const siteTitle = props.siteTitle;
  const [theme, setTheme] = createSignal("light");
  const [sidebarOpen, setSidebarOpen] = createSignal(false);
  const [tocOpen, setTocOpen] = createSignal(false);

  var themeBtnRef: HTMLElement | null = null;
  var sideBtnRef: HTMLElement | null = null;
  var overlayRef: HTMLElement | null = null;
  var sidebarRef: HTMLElement | null = null;
  var tocBtnRef: HTMLElement | null = null;
  var tocRef: HTMLElement | null = null;

  onMount(function () {
    var saved = "";
    try {
      saved = localStorage.getItem("theme") || "";
    } catch (e) {}
    var dark = saved === "dark";
    if (!dark && saved !== "light") {
      dark = window.matchMedia("(prefers-color-scheme: dark)").matches;
    }
    setTheme(dark ? "dark" : "light");

    var mq = window.matchMedia("(min-width: 1280px)");
    function onDesktop(e: MediaQueryListEvent) {
      if (e.matches) {
        setSidebarOpen(false);
        setTocOpen(false);
      }
    }
    if (mq.addEventListener) mq.addEventListener("change", onDesktop);
    else if (mq.addListener) mq.addListener(onDesktop);

    window.addEventListener("keydown", onKeydown);
  });

  createEffect(function () {
    var isDark = theme() === "dark";
    if (typeof document !== "undefined" && document.documentElement) {
      document.documentElement.setAttribute("data-theme", isDark ? "dark" : "light");
    }
    if (themeBtnRef) {
      themeBtnRef.setAttribute("aria-label", isDark ? "Switch to light mode" : "Switch to dark mode");
      themeBtnRef.setAttribute("data-theme-state", isDark ? "dark" : "light");
    }
  });

  createEffect(function () {
    var open = sidebarOpen();
    if (sidebarRef) sidebarRef.classList.toggle("open", open);
    if (overlayRef) overlayRef.classList.toggle("open", open);
    if (sideBtnRef) sideBtnRef.setAttribute("aria-expanded", open ? "true" : "false");
    if (open) setTocOpen(false);
  });

  createEffect(function () {
    var open = tocOpen();
    if (tocRef) tocRef.classList.toggle("open", open);
    if (tocBtnRef) tocBtnRef.setAttribute("aria-expanded", open ? "true" : "false");
    if (open) setSidebarOpen(false);
  });

  function onKeydown(e: KeyboardEvent) {
    if (e.key === "Escape") {
      setSidebarOpen(false);
      setTocOpen(false);
    }
  }

  function closeNav() {
    setSidebarOpen(false);
    setTocOpen(false);
  }

  function toggleTheme() {
    var next = theme() === "dark" ? "light" : "dark";
    setTheme(next);
    try {
      localStorage.setItem("theme", next);
    } catch (e) {}
  }

  return (
    <div class="docs-page">
      <Head>
        <title>{pageTitle} - {siteTitle}</title>
      </Head>

      <header class="docs-navbar">
        <a class="navbar-title" href="/docs/">{siteTitle}</a>
        <div class="navbar-actions">
          <div class="navbar-social-links">
            <SocialLinks links={props.socialLinks} />
          </div>
          <button class="theme-toggle" id="theme-toggle" ref={themeBtnRef} aria-label="Toggle light mode" type="button" onClick={toggleTheme}>
            <Icon name="tabler:sun" width="16" height="16" />
            <Icon name="tabler:moon" width="16" height="16" />
          </button>
          <button class="sidebar-toggle" id="sidebar-toggle" ref={sideBtnRef} aria-label="Open navigation" aria-controls="sidebar" aria-expanded="false" onClick={() => setSidebarOpen(!sidebarOpen())}>
            <Icon name="tabler:menu" width="20" height="20" />
          </button>
        </div>
      </header>

      <div class="toc-mobile-shell">
        <button class="toc-mobile-toggle" id="toc-toggle" ref={tocBtnRef} aria-label="Toggle table of contents" aria-controls="toc" aria-expanded="false" onClick={() => setTocOpen(!tocOpen())}>
          <span class="toc-mobile-copy">
            <span class="toc-mobile-label">On this page</span>
            <span class="toc-current" id="toc-current">Introduction</span>
          </span>
          <Icon name="tabler:chevron-down" width="18" height="18" />
        </button>
      </div>

      <div class="sidebar-overlay" id="sidebar-overlay" ref={overlayRef} onClick={closeNav}></div>

      <nav class="sidebar" id="sidebar" ref={sidebarRef}>
        <div class="sidebar-header">
          <div class="sidebar-header-row">
            <div class="sidebar-social-links">
              <SocialLinks links={props.socialLinks} />
            </div>
            <button class="sidebar-close" id="sidebar-close" aria-label="Close navigation" onClick={() => setSidebarOpen(false)}>
              <Icon name="tabler:x" width="18" height="18" />
            </button>
          </div>
        </div>
        <SidebarNav items={props.sidebarItems} currentPath={props.currentPath} onNavigate={closeNav} />
      </nav>

      <div class="docs-body">
        <main class="docs-main">
          <Breadcrumbs items={props.breadcrumbs} />
          <div class="docs-content">{props.children}</div>
          <PrevNext
            prevTitle={props.prevTitle}
            prevLink={props.prevLink}
            nextTitle={props.nextTitle}
            nextLink={props.nextLink}
          />
        </main>

        <aside class="toc" id="toc" ref={tocRef}>
          <div class="toc-panel">
            <div class="toc-header">
              <Icon name="lucide:text-align-start" width="16" height="16" />
              <span>On this page</span>
            </div>
            <TOCNav items={props.tocItems} onNavigate={closeNav} />
          </div>
        </aside>
      </div>
    </div>
  );
}