import { fireEvent, screen, waitFor } from '@testing-library/react';
import { expect, it, vi } from 'vitest';
import { ClientError } from '@/shared/api';
import { renderWithQuery } from '@/test/render';
import { MentorQuestionPanel } from './MentorQuestionPanel';
import { mentorQuestionsApi } from './api';

it.each([false, true])('explains an unconfirmed transfer after POST timeout (GET timeout: %s)', async (getTimeout) => {
  const get = vi.spyOn(mentorQuestionsApi, 'get').mockResolvedValueOnce({ question: null })
    .mockImplementationOnce(async () => {
      if (getTimeout) throw new ClientError('aborted', 'Запрос отменён', {
        cause: new DOMException('Timeout', 'TimeoutError'),
      });
      return { question: null };
    });
  const controller = new AbortController();
  const create = vi.spyOn(mentorQuestionsApi, 'create').mockImplementation(async () => {
    controller.abort(new DOMException('Timeout', 'TimeoutError'));
    throw new ClientError('aborted', 'Запрос отменён');
  });
  let timeout: ReturnType<typeof vi.spyOn> | undefined;
  try {
    renderWithQuery(<MentorQuestionPanel messageId="message_gap" canEscalate
      onOpenDocument={() => {}} onRepeatQuestion={() => {}} />);
    const button = await screen.findByRole('button', { name: 'Передать наставнику' });
    timeout = vi.spyOn(AbortSignal, 'timeout').mockReturnValueOnce(controller.signal);
    fireEvent.click(button);
    await waitFor(() => expect(get).toHaveBeenCalledTimes(2));
    await waitFor(() => expect(button).toBeEnabled());
    expect(await screen.findByRole('alert')).toHaveTextContent(/передач|подтвер|соединени/i);
  } finally {
    get.mockRestore();
    create.mockRestore();
    timeout?.mockRestore();
  }
});
