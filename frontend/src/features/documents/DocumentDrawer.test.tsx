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
