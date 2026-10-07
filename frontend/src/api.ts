export interface Summary {
  id: string;
  name: string;
  path: string;
  format: string;
  valid: boolean;
  keys: number;
  errors: number;
  warnings: number;
  empty: number;
  secrets: number;
}
export interface Entry {
  key: string;
  type: string;
  value: string;
  secret: boolean;
  empty: boolean;
  depth: number;
}
export interface Issue {
  severity: string;
  code: string;
  key: string;
  line: number;
  message: string;
}
export interface Query {
  page: number;
  size: number;
  search: string;
  filter: string;
  reveal: boolean;
}
export interface View {
  summary: Summary;
  entries: Entry[];
  diagnostics: Issue[];
  total: number;
  page: number;
  pages: number;
}
export interface Cell {
  file: string;
  present: boolean;
  invalid: boolean;
  entry: Entry;
}
export interface Comparison {
  baseline?: {
    actual: string;
    template: string;
    counts: {
      missing: number;
      present: number;
      extra: number;
      empty: number;
      unresolved: number;
      cycles: number;
    };
  };
  files: Summary[];
  rows: { key: string; status: string[]; cells: Cell[] }[];
  total: number;
  page: number;
  pages: number;
}
export interface Settings {
  theme: string;
  maskSensitive: boolean;
  rememberRecent: boolean;
  exportFormat: string;
  recursive: boolean;
  recent: string[];
}
declare global {
  interface Window {
    go: {
      main: {
        App: {
          GetSettings: () => Promise<Settings>;
          SaveSettings: (s: Settings) => Promise<void>;
          ClearRecent: () => Promise<void>;
          Files: () => Promise<Summary[]>;
          Clear: () => Promise<void>;
          Remove: (id: string) => Promise<void>;
          AddFiles: () => Promise<Summary[]>;
          AddFolder: () => Promise<Summary[]>;
          LoadPaths: (paths: string[]) => Promise<Summary[]>;
          Inspect: (id: string, q: Query) => Promise<View>;
          Compare: (q: Query) => Promise<Comparison>;
          CompareBaseline: (
            actual: string,
            baseline: string,
            q: Query,
          ) => Promise<Comparison>;
          PreviewBaselineReport: (
            actual: string,
            baseline: string,
            format: string,
          ) => Promise<string>;
          SaveBaselineReportTo: (
            actual: string,
            baseline: string,
            format: string,
            include: boolean,
            path: string,
            reviewed: boolean,
            confirmedValues: boolean,
          ) => Promise<string>;
          PreviewExample: (id: string, safe: boolean) => Promise<string>;
          SaveExample: (id: string, safe: boolean) => Promise<string>;
          SaveExampleTo: (
            id: string,
            safe: boolean,
            path: string,
            reviewed: boolean,
          ) => Promise<string>;
          ChooseDestination: (format: string) => Promise<string>;
          SaveReportTo: (
            format: string,
            include: boolean,
            path: string,
            reviewed: boolean,
            confirmedValues: boolean,
          ) => Promise<string>;
          PreviewReport: (format: string, include: boolean) => Promise<string>;
          SaveReport: (format: string, include: boolean) => Promise<string>;
        };
      };
    };
    runtime: {
      EventsOn: (name: string, callback: (...args: unknown[]) => void) => void;
      OnFileDrop: (
        callback: (x: number, y: number, paths: string[]) => void,
        useDropTarget: boolean,
      ) => void;
    };
  }
}
export const api = window.go.main.App;
