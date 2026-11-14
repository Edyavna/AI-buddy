import { http,HttpResponse } from 'msw';

// Legacy component fixtures have no linked history. Keep that explicit instead
// of inventing attempts or claiming a document was opened.
export function createQuestionContextHandlers(){return [
 http.get('*/api/v1/messages/:id/question-context',()=>HttpResponse.json({conversation_id:'mock-conversation',step:null,attempts:'',fragments:[],documents:[],limited:false})),
 http.post('*/api/v1/messages/:id/context-documents/:document/opened',()=>HttpResponse.json({opened:true})),
 ...['*/api/v1/messages/:id/context-documents/:document','*/api/v1/questions/:id/context-documents/:document','*/api/v1/mentor/questions/:id/context-documents/:document','*/api/v1/hr/questions/:id/context-documents/:document'].map(path=>http.get(path,({request,params})=>fetch(new URL('/api/v1/documents/'+encodeURIComponent(String(params.document)),request.url),{signal:request.signal,credentials:'include'})))
];}
