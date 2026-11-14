import { http,HttpResponse } from 'msw';

// Existing chat fixtures do not contain approved personal facts. Dedicated
// feature tests and real-Go acceptance exercise the actual selection flow.
export const personalMemoryHandlers=[
  http.get('*/api/v1/messages/:id/memory-proposals',()=>HttpResponse.json({state:'skipped',items:[]})),
  http.post('*/api/v1/messages/:id/memory-proposals',()=>HttpResponse.json({state:'skipped',items:[]})),
];
