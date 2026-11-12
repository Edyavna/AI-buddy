import { useState } from 'react';
import { expect, it } from 'vitest';
import { act, fireEvent, screen, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { renderWithQuery } from '@/test/render';
import { server } from '@/test/server';
import { createScenario } from '@/mocks/fixtures/scenario';
import { queryKeys } from '@/shared/api';
import { AccessRequestPanel } from './AccessRequestPanel';

const fixture = createScenario();
const key = queryKeys.request(fixture.draft.id);
function Harness() {
  const [visible, setVisible] = useState(true);
  return <><button onClick={() => setVisible(value => !value)}>Переключить карточку</button>
    {visible && <AccessRequestPanel requestId={fixture.draft.id} />}</>;
}

function expectRequestHidden() {
  expect(screen.queryByText(fixture.draft.resource.name)).not.toBeInTheDocument();
  expect(screen.queryByText(fixture.draft.reason)).not.toBeInTheDocument();
  expect(screen.queryByRole('button', { name: 'Подтвердить отправку' })).not.toBeInTheDocument();
}

it.each([401, 403, 404])('keeps revoked request details hidden after %s and a failed refresh, then recovers', async deniedStatus => {
  let status = 200;
  server.use(http.get('*/api/v1/access-requests/:id', () => status === 200
    ? HttpResponse.json(fixture.draft)
    : HttpResponse.json({ error: { code: 'ACCESS_REVOKED', message: 'Доступ к заявке отозван', trace_id: 't' } }, { status })));
  const { queryClient } = renderWithQuery(<AccessRequestPanel requestId={fixture.draft.id} />);
  expect(await screen.findByText(fixture.draft.resource.name)).toBeInTheDocument();

  status = deniedStatus;
  await act(async () => { await queryClient.refetchQueries({ queryKey: key }); });
  await waitFor(expectRequestHidden);
  expect(queryClient.getQueryData(key)).toBeNull();

  status = 503;
  await act(async () => { await queryClient.refetchQueries({ queryKey: key }); });
  await waitFor(expectRequestHidden);
  expect(queryClient.getQueryData(key)).toBeNull();

  status = 200;
  fireEvent.click(screen.getByRole('button', { name: 'Повторить' }));
  expect(await screen.findByText(fixture.draft.resource.name)).toBeInTheDocument();
  expect(screen.getByText(fixture.draft.reason)).toBeInTheDocument();
  expect(screen.getByRole('button', { name: 'Подтвердить отправку' })).toBeInTheDocument();
});

it.each([401, 403, 404])('clears cached request details when confirmation fails with %s', async deniedStatus => {
  let gets = 0;
  server.use(
    http.get('*/api/v1/access-requests/:id', () => {
      gets += 1;
      return gets === 1 ? HttpResponse.json(fixture.draft) : HttpResponse.json(
        { error: { code: 'SERVICE_UNAVAILABLE', message: 'Повторите позже', trace_id: 't' } },
        { status: 503 },
      );
    }),
    http.post('*/api/v1/access-requests/:id/confirm', () => HttpResponse.json(
      { error: { code: 'ACCESS_REVOKED', message: 'Доступ к заявке отозван', trace_id: 't' } },
      { status: deniedStatus },
    )),
  );
  const { queryClient } = renderWithQuery(<AccessRequestPanel requestId={fixture.draft.id} />);
  expect(await screen.findByText(fixture.draft.resource.name)).toBeInTheDocument();

  fireEvent.click(screen.getByRole('button', { name: 'Подтвердить отправку' }));
  await waitFor(() => expect(queryClient.getQueryState(key)?.status).toBe('error'));
  expectRequestHidden();
  expect(queryClient.getQueryData(key)).toBeNull();
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
  await waitFor(() => expect(queryClient.getQueryState(key)?.status).toBe('error'));
  expect(screen.getByText(fixture.draft.resource.name)).toBeInTheDocument();
  expect(screen.getByText('Черновик')).toBeInTheDocument();
  expect(screen.getByText(/Не удалось обновить статус/)).toBeInTheDocument();
});
