---
title: Hello, Collections
description: The first typed content collection entry.
date: 2026-09-12
author: Krate
tags: [content, collections, typescript]
draft: false
order: 1
---

# Hello, Collections

This file lives in `src/content/blog` and is validated against the `blog`
collection schema in `content.config.ts`. Because `order` is required, a build
fails if this entry omits it — and `.krate/types/content.d.ts` types the entry
for editors.
