import { useState } from 'react';
import { expect, it } from 'vitest';
import { fireEvent, screen, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { renderWithQuery } from '@/test/render';
import { server } from '@/test/server';
import { createScenario } from '@/mocks/fixtures/scenario';
import { queryKeys } from '@/shared/api';
import { AccessRequestPanel } from './AccessRequestPanel';

const fixture = createScenario();
function Harness() {
  const [visible, setVisible] = useState(true);
  return <><button onClick={() => setVisible(value => !value)}>Переключить карточку</button>
    {visible && <AccessRequestPanel requestId={fixture.draft.id} />}</>;
}

it.each([401, 403, 404])('hides cached request details and confirm after access failure %s', async status => {
  let allowed = true;
  server.use(http.get('*/api/v1/access-requests/:id', () => allowed
    ? HttpResponse.json(fixture.draft)
    : HttpResponse.json({ error: { code: 'ACCESS_REVOKED', message: 'Доступ к заявке отозван', trace_id: 't' } }, { status })));
  const { queryClient } = renderWithQuery(<Harness />);
  expect(await screen.findByText(fixture.draft.resource.name)).toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: 'Переключить карточку' }));
  allowed = false;
  fireEvent.click(screen.getByRole('button', { name: 'Переключить карточку' }));
  await waitFor(() => expect(queryClient.getQueryState(queryKeys.request(fixture.draft.id))?.status).toBe('error'));
  expect(screen.queryByText(fixture.draft.resource.name)).not.toBeInTheDocument();
  expect(screen.queryByText(fixture.draft.reason)).not.toBeInTheDocument();
  expect(screen.queryByRole('button', { name: 'Подтвердить отправку' })).not.toBeInTheDocument();
  expect(queryClient.getQueryData(queryKeys.request(fixture.draft.id))).toMatchObject({ id: fixture.draft.id });
});

it('retains the last known request on a temporary 503 without changing its status', async () => {
  let available = true;
  server.use(http.get('*/api/v1/access-requests/:id', () => available
    ? HttpResponse.json(fixture.draft)
    : HttpResponse.json({ error: { code: 'SERVICE_UNAVAILABLE', message: 'Повторите позже', trace_id: 't' } }, { status: 503 })));
  const { queryClient } = renderWithQuery(<Harness />);
  await screen.findByText(fixture.draft.resource.name);
  fireEvent.click(screen.getByRole('button', { name: 'Переключить карточку' }));
  available = false;
  fireEvent.click(screen.getByRole('button', { name: 'Переключить карточку' }));
  await waitFor(() => expect(queryClient.getQueryState(queryKeys.request(fixture.draft.id))?.status).toBe('error'));
  expect(screen.getByText(fixture.draft.resource.name)).toBeInTheDocument();
  expect(screen.getByText('Черновик')).toBeInTheDocument();
  expect(screen.getByText(/Не удалось обновить статус/)).toBeInTheDocument();
});
