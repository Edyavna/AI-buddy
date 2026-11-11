import { useRef, useState, type ReactNode } from 'react';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { Button } from '@/shared/ui';
import styles from './MarkdownContent.module.css';

// Explicitly restrict protocols, including schemes obscured with control characters.
export function safeMarkdownUrl(value: string): string {
  const compact = value.replace(/[\u0000-\u0020\u007f]/g, '');
  const scheme = compact.match(/^([a-z][a-z0-9+.-]*):/i)?.[1]?.toLowerCase();
  if (scheme && !['http', 'https', 'mailto'].includes(scheme)) return '';
  if (compact.startsWith('//') || compact.includes('\\')) return '';
  return value;
}

function CodeBlock({ children }: { children?: ReactNode }) {
  const ref = useRef<HTMLPreElement>(null);
  const [notice, setNotice] = useState('');
  async function copy() {
    try {
      await navigator.clipboard.writeText(ref.current?.textContent ?? '');
      setNotice('Код скопирован');
    } catch { setNotice('Не удалось скопировать код. Выделите и скопируйте его вручную.'); }
  }
  return <div className={styles.codeBlock}>
    <div className={styles.codeActions}><Button size="sm" variant="secondary" onClick={() => void copy()}>Копировать код</Button></div>
    <pre ref={ref} tabIndex={0} aria-label="Блок кода">{children}</pre>
    {notice && <span role="status">{notice}</span>}
  </div>;
}

export function MarkdownContent({ content }: { content: string }) {
  return <div className={styles.markdown}>
    <ReactMarkdown skipHtml remarkPlugins={[remarkGfm]} urlTransform={safeMarkdownUrl} components={{
      pre: ({ children }) => <CodeBlock>{children}</CodeBlock>,
      a: ({ href, children }) => href
        ? <a href={href} target="_blank" rel="noopener noreferrer">{children}</a>
        : <span>{children}</span>,
      // Do not fetch arbitrary remote images embedded in model output/documents.
      img: ({ alt }) => <span>{alt || 'Изображение'}</span>,
      table: ({ children }) => <div className={styles.tableScroll}><table>{children}</table></div>,
    }}>{content}</ReactMarkdown>
  </div>;
}
