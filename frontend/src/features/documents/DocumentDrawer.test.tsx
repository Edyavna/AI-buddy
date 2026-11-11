import { useCallback, useState } from 'react';
import { expect, it } from 'vitest';
import { fireEvent, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { renderWithQuery } from '@/test/render';
import { server } from '@/test/server';
import { DocumentDrawer } from './DocumentDrawer';

function Harness({ id = 'doc_local_setup' }: { id?: string }) {
  const [active, setActive] = useState<string | null>(null);
  const close = useCallback(() => setActive(null), []);
  return <><button onClick={() => setActive(id)}>Открыть источник</button><DocumentDrawer documentId={active} onClose={close} /></>;
}
it('loads only when opened, traps keyboard focus and restores focus on Escape', async () => {
  const user = userEvent.setup();
  const { queryClient } = renderWithQuery(<Harness />);
  expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
  expect(queryClient.getQueryData(['documents', 'doc_local_setup'])).toBeUndefined();
  await user.click(screen.getByRole('button', { name: 'Открыть источник' }));
  expect(await screen.findByText(/Синтетическая инструкция/)).toBeInTheDocument();
  const close = screen.getByRole('button', { name: 'Закрыть панель' });
  await waitFor(() => expect(close).toHaveFocus());
  await user.tab({ shift: true });
  expect(close).toHaveFocus();
  await user.tab(); expect(close).toHaveFocus();
  await user.keyboard('{Escape}');
  expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
  expect(screen.getByRole('button', { name: 'Открыть источник' })).toHaveFocus();
});
it('reports 404 without showing stale content', async () => {
  renderWithQuery(<Harness id="missing" />);
  fireEvent.click(screen.getByRole('button', { name: 'Открыть источник' }));
  expect(await screen.findByRole('alert')).toHaveTextContent('Документ недоступен или удалён');
});

it.each([401, 403, 404])('hides cached document title and content after reopening returns %s', async status => {
  let allowed = true;
  const cachedDocument = {
    id: 'doc_local_setup', title: 'Закрытое название документа',
    content: 'Закрытый текст документа', updated_at: '2026-11-09T12:00:00Z',
  };
  server.use(http.get('*/api/v1/documents/:id', () => allowed
    ? HttpResponse.json(cachedDocument)
    : HttpResponse.json({ error: { code: 'ACCESS_REVOKED', message: 'Документ недоступен', trace_id: 't' } }, { status })));
  const { queryClient } = renderWithQuery(<Harness />);
  fireEvent.click(screen.getByRole('button', { name: 'Открыть источник' }));
  expect(await screen.findByText(cachedDocument.content)).toBeInTheDocument();
  expect(screen.getByRole('dialog')).toHaveAccessibleName(cachedDocument.title);
  expect(queryClient.getQueryData(['documents', cachedDocument.id])).toEqual(cachedDocument);

  fireEvent.click(screen.getByRole('button', { name: 'Закрыть панель' }));
  allowed = false;
  fireEvent.click(screen.getByRole('button', { name: 'Открыть источник' }));
  expect(await screen.findByRole('alert')).toHaveTextContent('Документ недоступен');
  // React Query deliberately retains old data after a refetch error.
  expect(queryClient.getQueryData(['documents', cachedDocument.id])).toEqual(cachedDocument);
  expect(screen.queryByText(cachedDocument.content)).not.toBeInTheDocument();
  expect(screen.queryByText(cachedDocument.title)).not.toBeInTheDocument();
  expect(screen.getByRole('dialog')).toHaveAccessibleName('Источник ответа');

  fireEvent.click(screen.getByRole('button', { name: 'Закрыть панель' }));
  allowed = true;
  fireEvent.click(screen.getByRole('button', { name: 'Открыть источник' }));
  expect(await screen.findByText(cachedDocument.content)).toBeInTheDocument();
  expect(screen.getByRole('dialog')).toHaveAccessibleName(cachedDocument.title);
});

it('wraps keyboard focus between the close control and the final document link', async () => {
  server.use(http.get('*/api/v1/documents/:id', () => HttpResponse.json({
    id: 'doc_local_setup', title: 'Инструкция', content: '```sh\necho test\n```\n\n[Открыть справку](https://example.org)', updated_at: '2026-11-09T12:00:00Z',
  })));
  const user = userEvent.setup();
  renderWithQuery(<Harness />);
  await user.click(screen.getByRole('button', { name: 'Открыть источник' }));
  const link = await screen.findByRole('link', { name: 'Открыть справку' });
  const close = screen.getByRole('button', { name: 'Закрыть панель' });
  await waitFor(() => expect(close).toHaveFocus());
  await user.tab({ shift: true }); expect(link).toHaveFocus();
  await user.tab(); expect(close).toHaveFocus();
  await user.keyboard('{Escape}');
  expect(screen.getByRole('button', { name: 'Открыть источник' })).toHaveFocus();
});
it('shows protocol errors and permits explicit retry', async () => {
  let correct = false;
  server.use(http.get('*/api/v1/documents/:id', () => HttpResponse.json(correct ? {
    id: 'doc_local_setup', title: 'Тестовый документ', content: 'Рабочая инструкция', updated_at: '2026-11-09T12:00:00Z',
  } : { broken: true })));
  renderWithQuery(<Harness />);
  fireEvent.click(screen.getByRole('button', { name: 'Открыть источник' }));
  expect(await screen.findByRole('alert')).toHaveTextContent('не соответствует контракту');
  correct = true;
  fireEvent.click(screen.getByRole('button', { name: 'Повторить' }));
  expect(await screen.findByText('Рабочая инструкция')).toBeInTheDocument();
});
