/**
 * Manual mock for the 'vscode' module.
 * Provides the minimal surface that extension code under test references.
 * Jest maps 'vscode' → this file via jest.config.js moduleNameMapper.
 */

export enum TreeItemCollapsibleState {
  None = 0,
  Collapsed = 1,
  Expanded = 2,
}

export class TreeItem {
  label: string | undefined;
  description: string | undefined;
  tooltip: string | undefined;
  contextValue: string | undefined;
  iconPath: ThemeIcon | undefined;
  command: { command: string; title: string; arguments?: unknown[] } | undefined;
  collapsibleState: TreeItemCollapsibleState;

  constructor(label: string, collapsibleState?: TreeItemCollapsibleState) {
    this.label = label;
    this.collapsibleState = collapsibleState ?? TreeItemCollapsibleState.None;
  }
}

export class ThemeIcon {
  constructor(public readonly id: string) {}
}

export class EventEmitter<T> {
  private listeners: Array<(e: T) => void> = [];

  get event(): (listener: (e: T) => void) => { dispose: () => void } {
    return (listener) => {
      this.listeners.push(listener);
      return {
        dispose: () => {
          this.listeners = this.listeners.filter((l) => l !== listener);
        },
      };
    };
  }

  fire(data: T): void {
    for (const l of this.listeners) l(data);
  }

  dispose(): void {
    this.listeners = [];
  }
}

export class Uri {
  static file(path: string): Uri {
    return new Uri('file', '', path, '', '');
  }
  constructor(
    public readonly scheme: string,
    public readonly authority: string,
    public readonly path: string,
    public readonly query: string,
    public readonly fragment: string,
  ) {}
  get fsPath(): string { return this.path; }
}

// Mutable config store for tests to manipulate.
const _configStore: Record<string, unknown> = {
  'avc.cliPath': 'avc',
  'avc.projectPath': '',
  'avc.defaultAgentName': '',
  'avc.autoSnapshot.enabled': false,
  'avc.autoSnapshot.debounceSeconds': 30,
  'avc.autoSnapshot.cooldownMinutes': 5,
};

export function __setConfig(key: string, value: unknown): void {
  _configStore[key] = value;
}

export function __resetConfig(): void {
  Object.keys(_configStore).forEach((k) => delete _configStore[k]);
  Object.assign(_configStore, {
    'avc.cliPath': 'avc',
    'avc.projectPath': '',
    'avc.defaultAgentName': '',
    'avc.autoSnapshot.enabled': false,
    'avc.autoSnapshot.debounceSeconds': 30,
    'avc.autoSnapshot.cooldownMinutes': 5,
  });
}

const _workspaceFolders: Array<{ uri: Uri }> = [];

export function __setWorkspaceFolders(folders: Array<{ uri: Uri }>): void {
  _workspaceFolders.length = 0;
  _workspaceFolders.push(...folders);
}

export const workspace = {
  getConfiguration: jest.fn((section?: string) => {
    return {
      get: jest.fn(<T>(key: string, defaultValue?: T): T => {
        const fullKey = section ? `${section}.${key}` : key;
        const stored = _configStore[fullKey];
        return (stored !== undefined ? stored : defaultValue) as T;
      }),
    };
  }),

  get workspaceFolders(): Array<{ uri: Uri }> | undefined {
    return _workspaceFolders.length > 0 ? _workspaceFolders : undefined;
  },

  onDidSaveTextDocument: jest.fn((_handler: () => void) => ({
    dispose: jest.fn(),
  })),
};

export const window = {
  showErrorMessage: jest.fn((_msg: string) => Promise.resolve(undefined)),
  showInformationMessage: jest.fn((_msg: string) => Promise.resolve(undefined)),
  showWarningMessage: jest.fn((_msg: string) => Promise.resolve(undefined)),
  showInputBox: jest.fn(() => Promise.resolve(undefined)),
  showQuickPick: jest.fn(() => Promise.resolve(undefined)),
  createStatusBarItem: jest.fn(() => ({
    text: '',
    tooltip: '',
    show: jest.fn(),
    hide: jest.fn(),
    dispose: jest.fn(),
  })),
};

export const commands = {
  executeCommand: jest.fn((_command: string, ..._args: unknown[]) =>
    Promise.resolve(undefined)
  ),
  registerCommand: jest.fn((_command: string, _handler: unknown) => ({
    dispose: jest.fn(),
  })),
};
