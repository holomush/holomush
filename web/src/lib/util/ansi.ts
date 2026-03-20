const ANSI_REGEX = /\x1b\[/;
export function hasAnsiCodes(text: string): boolean {
  return ANSI_REGEX.test(text);
}
