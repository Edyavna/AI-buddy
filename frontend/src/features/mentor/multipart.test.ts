// @vitest-environment node
import { expect, it, beforeAll, afterAll, vi } from 'vitest';
import { server } from '@/test/server';
import { createMentorMock } from '@/mocks/mentor/handlers';
import { mentorApi } from './api';
beforeAll(() => {
  const interceptedFetch = globalThis.fetch;
  vi.spyOn(globalThis, 'fetch').mockImplementation((input, init) => interceptedFetch(typeof input === 'string' ? new URL(input, 'http://localhost') : input, init));
});
afterAll(() => vi.restoreAllMocks());
const signal = () => new AbortController().signal;
const form = (id = crypto.randomUUID()) => {
  const body = new FormData(); body.set('upload_id', id); body.set('title', 'Инструкция'); body.set('file', new File(['# Инструкция'], 'guide.md')); body.set('question_id', 'gap_demo_1'); return body;
};
it('sends real multipart, deduplicates uploads and rejects resolution before ready', async () => {
  const mock = createMentorMock('processing'); mock.control.setRole('mentor'); server.use(...mock.handlers);
  const body = form(), doc = await mentorApi.upload(body, signal());
  expect(doc.status).toBe('processing');
  expect((await mentorApi.upload(body, signal())).id).toBe(doc.id);
  body.set('title', 'Другой документ');
  await expect(mentorApi.upload(body, signal())).rejects.toMatchObject({ status: 409, code: 'IDEMPOTENCY_CONFLICT' });
  const q = await mentorApi.question('gap_demo_1', signal());
  expect(q.status).toBe('new');
  await expect(mentorApi.updateQuestion(q.id, { status: 'resolved', revision: q.revision, resolution: 'Решение', document_ids: [doc.id] }, signal())).rejects.toMatchObject({ code: 'DOCUMENT_NOT_READY' });
});
it('recovers a lost upload response with the same id and retries failed indexing', async () => {
  const mock = createMentorMock('lost_upload'); mock.control.setRole('mentor'); server.use(...mock.handlers);
  const body = form();
  await expect(mentorApi.upload(body, signal())).rejects.toMatchObject({ kind: 'network' });
  const doc = await mentorApi.upload(body, signal());
  expect(mock.control.documents).toHaveLength(2);
  await mentorApi.documents(signal()); await mentorApi.documents(signal());
  expect(mock.control.documents[1]?.status).toBe('ready');
  await expect(mentorApi.retryDocument(doc.id, signal())).rejects.toMatchObject({ status: 409 });
  mock.control.documents[1]!.status = 'failed';
  expect((await mentorApi.retryDocument(doc.id, signal())).status).toBe('processing');
  expect((await mentorApi.retryDocument(doc.id, signal())).status).toBe('processing');
});
