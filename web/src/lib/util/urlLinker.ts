const URL_REGEX = /(https?:\/\/[^\s<>"']+)/g;
export function linkUrls(text: string): string {
  return text.replace(URL_REGEX, '<a href="$1" target="_blank" rel="noopener noreferrer">$1</a>');
}
