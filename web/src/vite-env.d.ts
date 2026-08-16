/// <reference types="vite/client" />

declare module 'foliate-js/view.js';

type FoliateRelocateDetail = {
  cfi?: unknown;
  fraction?: unknown;
};

type FoliateRendererElement = HTMLElement & {
  setStyles?: (styles: string | string[]) => void;
};

type FoliateViewElement = HTMLElement & {
  renderer?: FoliateRendererElement;
  open: (book: string) => Promise<void>;
  init: (options: { lastLocation?: string; showTextStart?: boolean }) => Promise<void>;
  goTo: (target: string | number | { fraction: number }) => Promise<unknown>;
  prev: () => Promise<void>;
  next: () => Promise<void>;
  close?: () => void;
};
