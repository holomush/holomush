function escapeHtml(text: string): string {
  return text
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#039;');
}

const URL_REGEX = /(https?:\/\/[^\s<>"']+)/g;
export function linkUrls(text: string): string {
  const escaped = escapeHtml(text);
  return escaped.replace(URL_REGEX, '<a href="$1" target="_blank" rel="noopener noreferrer">$1</a>');
}
