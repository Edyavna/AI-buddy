import { afterEach, expect, it, vi } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';
import { MarkdownContent, safeMarkdownUrl } from './MarkdownContent';

afterEach(() => vi.unstubAllGlobals());

it('renders Markdown, GFM and code while omitting raw HTML and remote images', () => {
  const { container } = render(<MarkdownContent content={'## Инструкция\n\n**Важно** и `read`\n\n- Первый шаг\n\n| A | B |\n| - | - |\n| 1 | 2 |\n\n<script>window.evil = true</script>\n\n<img src="x" onerror="alert(1)">\n\n![Схема](https://example.org/tracker.png)'} />);
  expect(screen.getByRole('heading', { name: 'Инструкция' })).toBeInTheDocument();
  expect(container.querySelector('strong')).toHaveTextContent('Важно');
  expect(container.querySelector('table')).toBeInTheDocument();
  expect(container.querySelector('script, img')).toBeNull();
  expect(screen.getByText('Схема')).toBeInTheDocument();
});

it.each(['javascript:alert(1)', 'JaVaScRiPt:alert(1)', 'java\nscript:alert(1)', 'data:text/html,x', 'vbscript:x', 'file:///secret', '//evil.test/x', '\\evil.test'])('blocks unsafe URL %j', url => {
  expect(safeMarkdownUrl(url)).toBe('');
});
it.each(['https://example.org', 'http://example.org', 'mailto:team@example.org', '/docs/setup', '#setup'])('allows safe URL %s', url => {
  expect(safeMarkdownUrl(url)).toBe(url);
});
it('renders unsafe links as text and safe links without opener access', () => {
  render(<MarkdownContent content={'[Опасно](javascript:alert) [Полезно](https://example.org)'} />);
  expect(screen.queryByRole('link', { name: 'Опасно' })).not.toBeInTheDocument();
  expect(screen.getByRole('link', { name: 'Полезно' })).toHaveAttribute('rel', 'noopener noreferrer');
});
it('copies exact code content and reports clipboard failure', async () => {
  const writeText = vi.fn().mockResolvedValue(undefined);
  vi.stubGlobal('navigator', { clipboard: { writeText } });
  render(<MarkdownContent content={'```sh\ngit clone example\n```'} />);
  fireEvent.click(screen.getByRole('button', { name: 'Копировать код' }));
  expect(await screen.findByRole('status')).toHaveTextContent('Код скопирован');
  expect(writeText).toHaveBeenCalledWith('git clone example\n');
  writeText.mockRejectedValueOnce(new Error('denied'));
  fireEvent.click(screen.getByRole('button', { name: 'Копировать код' }));
  expect(await screen.findByText(/Не удалось скопировать/)).toBeInTheDocument();
});
