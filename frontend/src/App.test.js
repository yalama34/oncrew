import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import App from './App';

const response = data => Promise.resolve({ ok: true, json: () => Promise.resolve(data) });

afterEach(() => jest.restoreAllMocks());

test('shows real runs and filters them by status', async () => {
  global.fetch = jest.fn(url => response(url.endsWith('/agents') ? [] : [{ id: 'one', goal: 'Проверить API', status: 'finished', lead_id: 'lead', agent_ids: [], created_at: '2026-10-30T12:00:00Z', tasks_total: 2, tasks_done: 1, tasks_review: 0 }]));
  render(<App />);
  expect(await screen.findByText('Проверить API', { selector: 'strong' })).toBeInTheDocument();
  expect(screen.getByText('50%')).toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: /Ошибка/ }));
  expect(screen.getByText('Ничего не найдено')).toBeInTheDocument();
});

test('shows a connection error when API is unavailable', async () => {
  global.fetch = jest.fn(() => Promise.reject(new Error('offline')));
  render(<App />);
  expect(await screen.findByRole('alert')).toHaveTextContent('Не удалось подключиться к API');
});

test('shows the lead question and submits an answer', async () => {
  const run = { id: 'run-1', goal: 'Анализ', status: 'waiting', lead_id: 'lead', agent_ids: [], created_at: '2026-10-30T12:00:00Z', tasks_total: 0, tasks_done: 0, tasks_review: 0 };
  global.fetch = jest.fn((url, options) => {
    if (url.endsWith('/answers')) return response({ status: 'running' });
    if (url.endsWith('/agents')) return response([]);
    if (url.endsWith('/questions')) return response([{ id: 'q-1', question: 'Какие данные использовать?', answer: null }]);
    if (url.endsWith('/tasks') || url.endsWith('/artifacts')) return response([]);
    return response([run]);
  });
  render(<App />);
  fireEvent.click(await screen.findByRole('button', { name: /Анализ/ }));
  expect(await screen.findByText('Какие данные использовать?')).toBeInTheDocument();
  fireEvent.change(screen.getByLabelText('Ваш ответ'), { target: { value: 'Используй таблицу' } });
  fireEvent.click(screen.getByRole('button', { name: /Ответить и продолжить/ }));
  await waitFor(() => expect(global.fetch).toHaveBeenCalledWith('/api/runs/run-1/answers', expect.objectContaining({ method: 'POST', body: JSON.stringify({ question_id: 'q-1', answer: 'Используй таблицу' }) })));
});

test('shows model activity and task failure', async () => {
  const run = { id: 'stream-run', goal: 'Проверка потока', status: 'running', lead_id: 'lead', agent_ids: [], created_at: '2026-10-30T12:00:00Z', tasks_total: 2, tasks_done: 0, tasks_review: 0 };
  global.fetch = jest.fn(url => {
    if (url.endsWith('/agents')) return response([]);
    if (url.endsWith('/tasks')) return response([
      { id: 't1', text: 'Пишет код', status: 'in_progress', model_phase: 'receiving', model_started_at: new Date().toISOString(), model_last_activity_at: new Date().toISOString() },
      { id: 't2', text: 'Ошибка модели', status: 'blocked', failure_reason: 'Модель не присылала данные 60 секунд.' }
    ]);
    if (url.endsWith('/questions') || url.endsWith('/artifacts')) return response([]);
    return response([run]);
  });
  render(<App />);
  fireEvent.click(await screen.findByRole('button', { name: /Проверка потока/ }));
  expect(await screen.findByText('Модель отвечает')).toBeInTheDocument();
  expect(screen.getByText(/Последние данные:/)).toBeInTheDocument();
  expect(screen.getByText('Модель не присылала данные 60 секунд.')).toBeInTheDocument();
});

test('submits the specification text with the new run', async () => {
  const agents = [{ id:'lead', name:'Lead', role:'teamlead', model:'model' }, { id:'worker', name:'Backend', role:'backend', model:'model' }];
  global.fetch = jest.fn((url, options) => {
    if (url.endsWith('/agents')) return response(agents);
    if (url.endsWith('/runs') && options?.method === 'POST') return response({ id:'new-run' });
    if (url.endsWith('/tasks') || url.endsWith('/artifacts') || url.endsWith('/questions')) return response([]);
    return response([]);
  });
  render(<App />);
  fireEvent.click(await screen.findByRole('button', { name:/Новый запуск/ }));
  fireEvent.change(screen.getByLabelText('Цель запуска'), { target:{ value:'Сделать магазин' } });
  fireEvent.change(screen.getByLabelText('Контекст задачи (Markdown)'), { target:{ value:'# Оплата\nТолько картой' } });
  fireEvent.change(screen.getByLabelText('Тимлид'), { target:{ value:'lead' } });
  fireEvent.click(screen.getByRole('checkbox'));
  fireEvent.click(screen.getByRole('button', { name:/Создать запуск/ }));
  await waitFor(() => expect(global.fetch).toHaveBeenCalledWith('/api/runs', expect.objectContaining({ method:'POST', body:JSON.stringify({ goal:'Сделать магазин', lead_id:'lead', agent_ids:['worker'], spec_text:'# Оплата\nТолько картой', spec_name:'' }) })));
});

test('shows a specification indexing error', async () => {
  const run = { id:'failed-spec', goal:'Проект', status:'failed', lead_id:'lead', agent_ids:[], created_at:'2026-10-30T12:00:00Z', tasks_total:0, tasks_done:0, tasks_review:0, spec_name:'spec.md', spec_status:'failed', spec_error:'Проверьте LM Studio.' };
  global.fetch = jest.fn(url => { if (url.endsWith('/agents') || url.endsWith('/tasks') || url.endsWith('/artifacts') || url.endsWith('/questions')) return response([]); return response([run]); });
  render(<App />);
  fireEvent.click(await screen.findByRole('button', { name:/Проект/ }));
  expect(await screen.findByText('Ошибка подготовки ТЗ')).toBeInTheDocument();
  expect(screen.getByText('Проверьте LM Studio.')).toBeInTheDocument();
});

test('shows task dependencies and their statuses', async () => {
  const run = { id:'deps-run', goal:'Зависимости', status:'running', lead_id:'lead', agent_ids:[], created_at:'2026-10-30T12:00:00Z', tasks_total:2, tasks_done:0, tasks_review:0 };
  global.fetch = jest.fn(url => {
    if (url.endsWith('/agents') || url.endsWith('/artifacts') || url.endsWith('/questions')) return response([]);
    if (url.endsWith('/tasks')) return response([{ id:'t', text:'Интеграция', status:'todo', dependencies:[{id:'a', text:'Создать API', status:'review'}] }]);
    return response([run]);
  });
  render(<App />);
  fireEvent.click(await screen.findByRole('button', { name:/Зависимости/ }));
  expect(await screen.findByText('Зависит от:')).toBeInTheDocument();
  expect(screen.getByText('Создать API · На ревью')).toBeInTheDocument();
});

test('shows failed task memory and retries indexing', async () => {
  const run = { id:'memory-run', goal:'Память', status:'finished', lead_id:'lead', agent_ids:[], created_at:'2026-10-30T12:00:00Z', tasks_total:1, tasks_done:1, tasks_review:0 };
  global.fetch = jest.fn((url, options) => {
    if (url.endsWith('/memory/retry')) return response({status:'pending'});
    if (url.endsWith('/agents') || url.endsWith('/artifacts') || url.endsWith('/questions')) return response([]);
    if (url.endsWith('/tasks')) return response([{id:'task-memory',text:'API',status:'done',memory_status:'failed',memory_error:'Модель недоступна.'}]);
    return response([run]);
  });
  render(<App />);
  fireEvent.click(await screen.findByRole('button', { name:/Память/ }));
  expect(await screen.findByText('Результат не попал в память')).toBeInTheDocument();
  expect(screen.getByText('Модель недоступна.')).toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name:'Повторить индексацию' }));
  await waitFor(() => expect(global.fetch).toHaveBeenCalledWith('/api/tasks/task-memory/memory/retry', expect.objectContaining({method:'POST'})));
});
