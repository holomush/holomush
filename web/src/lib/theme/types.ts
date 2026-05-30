// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

export interface ThemeColors {
  // MUSH message tokens (functional — distinct hues for readability)
  'say.speaker': string;
  'say.speech': string;
  'pose.actor': string;
  'pose.action': string;
  system: string;
  arrive: string;
  leave: string;
  'command.output': string;
  'command.error': string;
  ooc: string;
  pemit: string;

  // Chrome tokens (structural UI)
  background: string;
  foreground: string;
  input: string;
  surface: string;
  border: string;
  cursor: string;
  'input.prompt': string;
  'input.text': string;
  'input.background': string;
  'status.text': string;
  'status.background': string;
  'status.online': string;
  'status.offline': string;
  'sidebar.background': string;
  'scrollback.indicator': string;
  'scrollback.replayed': string;

  // shadcn semantic tokens
  primary: string;
  'primary.foreground': string;
  secondary: string;
  'secondary.foreground': string;
  muted: string;
  'muted.foreground': string;
  accent: string;
  'accent.foreground': string;
  destructive: string;
  'destructive.foreground': string;
  card: string;
  'card.foreground': string;
  popover: string;
  'popover.foreground': string;
  ring: string;
  radius: string;
}

export interface Theme {
  name: string;
  colors: ThemeColors;
}

export interface ThemePreferences {
  themeId: string;
  terminalBlackBackground: boolean;
}
