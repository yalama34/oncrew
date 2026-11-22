import { useEffect, useRef, useState } from 'react';

export default function ArtifactPreview({ runId, canStart, request, onShowFiles, children }) {
  const [session, setSession] = useState(null);
  const [starting, setStarting] = useState(false);
  const [message, setMessage] = useState('');
  const [error, setError] = useState('');
  const current = useRef(null);
  const generation = useRef(0);
  const panel = useRef(null);

  function closeSession(id) {
    return request(`/runs/${runId}/previews/${id}`, { method: 'DELETE', keepalive: true });
  }

  useEffect(() => () => {
    generation.current++;
    if (current.current) {
      request(`/runs/${runId}/previews/${current.current.id}`, { method: 'DELETE', keepalive: true }).catch(() => {});
      current.current = null;
    }
  }, [request, runId]);

  useEffect(() => {
    if (!session) return;
    panel.current?.scrollIntoView?.({ block: 'nearest' });
    const timer = setTimeout(() => {
      current.current = null;
      setSession(null);
      setMessage('Время превью истекло. Запустите результат снова.');
    }, Math.max(0, Date.parse(session.expires_at) - Date.now()));
    return () => clearTimeout(timer);
  }, [session]);

  async function start() {
    const attempt = ++generation.current;
    setStarting(true);
    setError('');
    setMessage('');
    try {
      const next = await request(`/runs/${runId}/previews`, { method: 'POST' });
      if (attempt !== generation.current) {
        closeSession(next.id).catch(() => {});
        return;
      }
      const url = new URL(next.url);
      if (!['http:', 'https:'].includes(url.protocol) || url.hostname === window.location.hostname ||
          url.username || url.password || !next.id || url.pathname !== `/p/${next.id}/index.html` ||
          url.search || url.hash || !Number.isFinite(Date.parse(next.expires_at))) {
        closeSession(next.id).catch(() => {});
        throw new Error('некорректный адрес или срок действия превью');
      }
      current.current = next;
      setSession(next);
    } catch (err) {
      if (attempt === generation.current) setError(`Не удалось запустить превью: ${err.message}`);
    } finally {
      if (attempt === generation.current) setStarting(false);
    }
  }

  function stop() {
    const id = session.id;
    const attempt = ++generation.current;
    current.current = null;
    setSession(null);
    closeSession(id).catch(() => {
      if (attempt === generation.current) {
        setMessage('Окно закрыто. Ссылка перестанет работать по истечении сессии.');
      }
    });
  }

  return <>
    <div className="section-head">
      <div><h2>Артефакты</h2><p>Файлы, созданные агентами</p></div>
      <div className="artifact-actions">
        {canStart && !session && <button className="secondary preview-action" type="button" onClick={start} disabled={starting}>
          <span aria-hidden="true">▶</span>
          {starting ? 'Запускаем…' : 'Запустить результат'}
        </button>}
        <button className="text-button" onClick={onShowFiles}>Все файлы →</button>
      </div>
    </div>
    {error && <div className="alert" role="alert">{error}</div>}
    {message && <p className="preview-message" role="status">{message}</p>}
    {children}
    {session && <section className="preview-panel" ref={panel} aria-label="Превью результата">
      <div className="preview-bar">
        <div>
          <strong>Превью результата</strong>
          <small>Сессия до {new Intl.DateTimeFormat('ru-RU', { hour: '2-digit', minute: '2-digit' }).format(new Date(session.expires_at))}</small>
        </div>
        <button className="text-button" type="button" onClick={stop}>Остановить</button>
      </div>
      <iframe title="Результат запуска" src={session.url} sandbox="allow-scripts" referrerPolicy="no-referrer" />
    </section>}
  </>;
}
