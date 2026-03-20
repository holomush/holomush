export interface ThemeColors {
  'say.speaker': string;
  'say.speech': string;
  'pose.actor': string;
  'pose.action': string;
  system: string;
  arrive: string;
  leave: string;
  background: string;
  surface: string;
  border: string;
  'input.prompt': string;
  'input.text': string;
  'input.background': string;
  'status.text': string;
  'status.background': string;
  'sidebar.background': string;
  'scrollback.indicator': string;
}

export interface Theme {
  name: string;
  colors: ThemeColors;
}
