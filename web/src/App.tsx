import { FormEvent, MouseEvent as ReactMouseEvent, TouchEvent as ReactTouchEvent, useEffect, useMemo, useRef, useState } from 'react';
import type { ReactNode } from 'react';
import type { PDFDocumentProxy } from 'pdfjs-dist';
import pdfWorkerURL from 'pdfjs-dist/build/pdf.worker.mjs?url';

import './styles.css';

let pdfjsPromise: Promise<typeof import('pdfjs-dist')> | null = null;

type Library = {
  id: string;
  name: string;
  created_at: string;
  updated_at: string;
};

type Book = {
  id: string;
  library_id: string;
  relative_path: string;
  folder: string;
  category: string;
  filename: string;
  extension: string;
  file_size: number;
  file_modified_at: string;
  page_count: number | null;
  status: 'discovered' | 'changed' | 'missing';
  created_at: string;
  updated_at: string;
};

type ScanSummary = {
  new: number;
  changed: number;
  unchanged: number;
  missing: number;
};

type ReadingProgress = {
  book_id: string;
  current_page: number;
  total_pages: number | null;
  locator: string | null;
  fraction: number | null;
  created_at?: string;
  updated_at?: string;
};

type BookAnnotation = {
  id: string;
  book_id: string;
  kind: 'bookmark' | 'note';
  page_number: number | null;
  total_pages: number | null;
  locator: string | null;
  fraction: number | null;
  note: string;
  created_at: string;
  updated_at: string;
};

type MetadataJob = {
  id: string;
  book_id: string;
  filename?: string;
  relative_path?: string;
  status: 'queued' | 'running' | 'succeeded' | 'failed';
  attempts: number;
  last_error: string | null;
  created_at: string;
  updated_at: string;
  completed_at: string | null;
};

type BookMetadata = {
  book_id: string;
  provider: string;
  provider_key: string;
  title: string;
  authors: string[];
  description: string;
  published_year: number | null;
  cover_url: string | null;
  source_url: string | null;
  confidence: number;
  created_at: string;
  updated_at: string;
};

type Folder = {
  path: string;
  name: string;
  parent_path: string;
  book_count: number;
};

type ApiError = {
  error?: {
    code?: string;
    message?: string;
  };
};

type AuthStatus = {
  enabled: boolean;
  authenticated: boolean;
  username?: string;
};

type Route =
  | { name: 'library' }
  | {
      name: 'details';
      bookId: string;
    }
  | {
      name: 'reader';
      bookId: string;
    };

type AppView = 'catalog' | 'settings' | 'metadata';
type AppTheme = 'dark' | 'light';

const pageSize = 50;
const pdfSettingsKey = 'alexandria.reader.pdf';
const ebookSettingsKey = 'alexandria.reader.ebook';
const appThemeKey = 'alexandria.theme';

type ReaderTheme = 'dark' | 'light' | 'sepia';
type EBookFlow = 'paginated' | 'scrolled';

type PDFSettings = {
  zoom: number;
  maxWidth: number;
  theme: ReaderTheme;
};

type PDFRenderTask = {
  cancel: () => void;
  promise: Promise<unknown>;
};

type EBookSettings = {
  zoom: number;
  maxWidth: number;
  flow: EBookFlow;
  theme: ReaderTheme;
  fontFamily: 'serif' | 'sans';
  lineHeight: number;
};

type ReaderTOCItem = {
  label: string;
  href: string;
  subitems?: ReaderTOCItem[];
};

const defaultPDFSettings: PDFSettings = {
  zoom: 1,
  maxWidth: 980,
  theme: 'dark',
};

const defaultEBookSettings: EBookSettings = {
  zoom: 1.1,
  maxWidth: 680,
  flow: 'paginated',
  theme: 'light',
  fontFamily: 'serif',
  lineHeight: 1.7,
};

function initialAppTheme(): AppTheme {
  return window.localStorage.getItem(appThemeKey) === 'light' ? 'light' : 'dark';
}

function toggleTheme(theme: AppTheme): AppTheme {
  return theme === 'dark' ? 'light' : 'dark';
}

function ThemeToggle({ theme, onToggleTheme }: { theme: AppTheme; onToggleTheme: () => void }) {
  const lightMode = theme === 'light';
  return (
    <button
      className="theme-toggle"
      type="button"
      onClick={onToggleTheme}
      aria-label={lightMode ? 'Ativar tema escuro' : 'Ativar tema claro'}
      title={lightMode ? 'Ativar tema escuro' : 'Ativar tema claro'}
    >
      <span aria-hidden="true">{lightMode ? '☾' : '☼'}</span>
      {lightMode ? 'Escuro' : 'Claro'}
    </button>
  );
}

export function App() {
  const [route, setRoute] = useState<Route>(() => currentRoute());
  const [authStatus, setAuthStatus] = useState<AuthStatus | null>(null);
  const [authLoading, setAuthLoading] = useState(true);
  const [theme, setTheme] = useState<AppTheme>(() => initialAppTheme());

  useEffect(() => {
    document.documentElement.dataset.theme = theme;
    window.localStorage.setItem(appThemeKey, theme);
  }, [theme]);

  useEffect(() => {
    function onPopState() {
      setRoute(currentRoute());
    }

    window.addEventListener('popstate', onPopState);
    return () => window.removeEventListener('popstate', onPopState);
  }, []);

  useEffect(() => {
    requestJSON<AuthStatus>('/api/auth/status')
      .then(setAuthStatus)
      .catch(() => setAuthStatus({ enabled: false, authenticated: true }))
      .finally(() => setAuthLoading(false));
  }, []);

  function navigate(path: string) {
    window.history.pushState(null, '', path);
    setRoute(currentRoute());
  }

  async function logout() {
    const nextStatus = await requestJSON<AuthStatus>('/api/auth/logout', { method: 'POST' });
    setAuthStatus(nextStatus);
    navigate('/');
  }

  if (authLoading) {
    return <div className="boot-state">Carregando Alexandria...</div>;
  }

  if (authStatus?.enabled && !authStatus.authenticated) {
    return <LoginScreen onAuthenticated={setAuthStatus} theme={theme} onToggleTheme={() => setTheme(toggleTheme)} />;
  }

  if (route.name === 'reader') {
    return <Reader bookId={route.bookId} onBack={() => navigate(`/books/${route.bookId}`)} />;
  }

  if (route.name === 'details') {
    return (
      <BookDetail
        bookId={route.bookId}
        onBack={() => navigate('/')}
        onLogout={authStatus?.enabled ? logout : undefined}
        onRead={(bookId) => navigate(`/reader/${bookId}`)}
        theme={theme}
        onToggleTheme={() => setTheme(toggleTheme)}
      />
    );
  }

  return (
    <LibraryScreen
      onLogout={authStatus?.enabled ? logout : undefined}
      onRead={(bookId) => navigate(`/books/${bookId}`)}
      theme={theme}
      onToggleTheme={() => setTheme(toggleTheme)}
    />
  );
}

function LoginScreen({
  onAuthenticated,
  theme,
  onToggleTheme,
}: {
  onAuthenticated: (status: AuthStatus) => void;
  theme: AppTheme;
  onToggleTheme: () => void;
}) {
  const [form, setForm] = useState({ username: 'admin', password: '' });
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');

  async function login(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setLoading(true);
    setError('');
    try {
      const status = await requestJSON<AuthStatus>('/api/auth/login', {
        method: 'POST',
        body: JSON.stringify(form),
      });
      onAuthenticated(status);
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setLoading(false);
    }
  }

  return (
    <main className="login-shell">
      <section className="login-panel">
        <div className="login-header">
          <div>
            <p className="eyebrow">Alexandria</p>
            <h1>Entrar</h1>
            <p>Acesse sua biblioteca pessoal.</p>
          </div>
          <ThemeToggle theme={theme} onToggleTheme={onToggleTheme} />
        </div>
        {error ? (
          <div className="alert" role="alert">
            {error}
          </div>
        ) : null}
        <form className="login-form" onSubmit={login}>
          <label>
            <span>Usuário</span>
            <input
              autoComplete="username"
              required
              value={form.username}
              onChange={(event) => setForm({ ...form, username: event.target.value })}
            />
          </label>
          <label>
            <span>Senha</span>
            <input
              autoComplete="current-password"
              required
              type="password"
              value={form.password}
              onChange={(event) => setForm({ ...form, password: event.target.value })}
            />
          </label>
          <button type="submit" disabled={loading}>
            {loading ? 'Entrando...' : 'Entrar'}
          </button>
        </form>
      </section>
    </main>
  );
}

function LibraryScreen({
  onLogout,
  onRead,
  theme,
  onToggleTheme,
}: {
  onLogout?: () => void;
  onRead: (bookId: string) => void;
  theme: AppTheme;
  onToggleTheme: () => void;
}) {
  const [view, setView] = useState<AppView>('catalog');
  const [libraries, setLibraries] = useState<Library[]>([]);
  const [selectedLibraryId, setSelectedLibraryId] = useState('');
  const [books, setBooks] = useState<Book[]>([]);
  const [folders, setFolders] = useState<Folder[]>([]);
  const [folderPath, setFolderPath] = useState('');
  const [statusFilter, setStatusFilter] = useState('');
  const [query, setQuery] = useState('');
  const [offset, setOffset] = useState(0);
  const [libraryLoading, setLibraryLoading] = useState(true);
  const [booksLoading, setBooksLoading] = useState(false);
  const [foldersLoading, setFoldersLoading] = useState(false);
  const [creating, setCreating] = useState(false);
  const [scanningId, setScanningId] = useState('');
  const [metadataLoadingId, setMetadataLoadingId] = useState('');
  const [coverRefreshToken, setCoverRefreshToken] = useState(0);
  const [error, setError] = useState('');
  const [metadataMessage, setMetadataMessage] = useState('');
  const [scanSummary, setScanSummary] = useState<ScanSummary | null>(null);
  const [form, setForm] = useState({ name: '', path: '' });

  const selectedLibrary = useMemo(
    () => libraries.find((library) => library.id === selectedLibraryId) ?? null,
    [libraries, selectedLibraryId],
  );

  const shelves = useMemo(() => groupBooksIntoShelves(books, folderPath), [books, folderPath]);

  async function loadLibraries(nextSelectedId = selectedLibraryId) {
    setLibraryLoading(true);
    try {
      const response = await requestJSON<{ items: Library[] }>('/api/libraries');
      setLibraries(response.items);
      if (response.items.length === 0) {
        setSelectedLibraryId('');
        setFolderPath('');
        return;
      }
      const selectedStillExists = response.items.some((item) => item.id === nextSelectedId);
      setSelectedLibraryId(selectedStillExists ? nextSelectedId : response.items[0].id);
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setLibraryLoading(false);
    }
  }

  async function loadBooks() {
    setBooksLoading(true);
    try {
      const params = new URLSearchParams({
        limit: String(pageSize),
        offset: String(offset),
      });
      if (selectedLibraryId) {
        params.set('library_id', selectedLibraryId);
      }
      if (statusFilter) {
        params.set('status', statusFilter);
      }
      if (query.trim()) {
        params.set('q', query.trim());
      }
      if (folderPath) {
        params.set('folder', folderPath);
      }

      const response = await requestJSON<{ items: Book[] }>(`/api/books?${params}`);
      setBooks(response.items);
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBooksLoading(false);
    }
  }

  async function loadFolders(libraryId = selectedLibraryId, parentPath = folderPath) {
    if (!libraryId) {
      setFolders([]);
      return;
    }

    setFoldersLoading(true);
    try {
      const params = new URLSearchParams();
      if (parentPath) {
        params.set('parent', parentPath);
      }
      const suffix = params.toString() ? `?${params}` : '';
      const response = await requestJSON<{ parent_path: string; items: Folder[] }>(
        `/api/libraries/${libraryId}/folders${suffix}`,
      );
      setFolders(response.items);
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setFoldersLoading(false);
    }
  }

  useEffect(() => {
    loadLibraries();
  }, []);

  useEffect(() => {
    loadBooks();
  }, [selectedLibraryId, statusFilter, query, folderPath, offset]);

  useEffect(() => {
    loadFolders();
  }, [selectedLibraryId, folderPath]);

  async function createLibrary(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setCreating(true);
    setError('');
    setScanSummary(null);

    try {
      const created = await requestJSON<Library>('/api/libraries', {
        method: 'POST',
        body: JSON.stringify(form),
      });
      setForm({ name: '', path: '' });
      setOffset(0);
      setFolderPath('');
      await loadLibraries(created.id);
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setCreating(false);
    }
  }

  async function deleteLibrary(id: string) {
    setError('');
    setScanSummary(null);
    try {
      await requestJSON<void>(`/api/libraries/${id}`, { method: 'DELETE' });
      setOffset(0);
      setFolderPath('');
      await loadLibraries('');
    } catch (err) {
      setError(errorMessage(err));
    }
  }

  async function scanLibrary(id: string) {
    setScanningId(id);
    setError('');
    setScanSummary(null);

    try {
      const summary = await requestJSON<ScanSummary>(`/api/libraries/${id}/scan`, {
        method: 'POST',
      });
      setScanSummary(summary);
      setOffset(0);
      await loadBooks();
      await loadFolders(id, folderPath);
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setScanningId('');
    }
  }

  async function refreshMetadata(bookId: string) {
    setMetadataLoadingId(bookId);
    setError('');
    setMetadataMessage('');
    try {
      await requestJSON<MetadataJob>(`/api/books/${bookId}/metadata/refresh`, {
        method: 'POST',
      });
      setMetadataMessage('Busca de metadados enfileirada. As capas aparecem quando o worker terminar.');
      window.setTimeout(() => setCoverRefreshToken((value) => value + 1), 8000);
      window.setTimeout(() => setCoverRefreshToken((value) => value + 1), 35000);
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setMetadataLoadingId('');
    }
  }

  function updateFilter(nextStatus: string) {
    setStatusFilter(nextStatus);
    setOffset(0);
  }

  function updateQuery(nextQuery: string) {
    setQuery(nextQuery);
    setOffset(0);
  }

  function selectFolder(nextFolderPath: string) {
    setFolderPath(nextFolderPath);
    setOffset(0);
    setScanSummary(null);
  }

  return (
    <main className="app-shell">
      <header className="topbar">
        <a className="brand" href="/" aria-label="Ir para o catálogo">
          <span className="brand-mark">A</span>
          <span className="brand-copy">
            <strong>Alexandria</strong>
            <small>{viewTitle(view)}</small>
          </span>
        </a>
        <nav className="app-nav" aria-label="Seções da aplicação">
          <button
            className={view === 'catalog' ? 'selected' : ''}
            type="button"
            onClick={() => setView('catalog')}
          >
            Catálogo
          </button>
          <button
            className={view === 'settings' ? 'selected' : ''}
            type="button"
            onClick={() => setView('settings')}
          >
            Configurações
          </button>
          <button
            className={view === 'metadata' ? 'selected' : ''}
            type="button"
            onClick={() => setView('metadata')}
          >
            Metadados
          </button>
        </nav>
        <div className="topbar-actions">
          <ThemeToggle theme={theme} onToggleTheme={onToggleTheme} />
          {onLogout ? (
            <button className="logout-button" type="button" onClick={onLogout}>
              Sair
            </button>
          ) : null}
        </div>
      </header>

      {error ? (
        <div className="alert" role="alert">
          {error}
        </div>
      ) : null}
      {metadataMessage ? <div className="notice">{metadataMessage}</div> : null}

      {view === 'catalog' ? (
        <section className="layout catalog-layout">
          <aside className="sidebar catalog-sidebar" aria-label="Filtros do catálogo">
            <LibraryPicker
              libraries={libraries}
              loading={libraryLoading}
              selectedLibraryId={selectedLibraryId}
              onSelect={(libraryId) => {
                setSelectedLibraryId(libraryId);
                setFolderPath('');
                setOffset(0);
                setScanSummary(null);
              }}
            />

            {selectedLibrary ? (
              <FolderPicker
                folders={folders}
                folderPath={folderPath}
                loading={foldersLoading}
                onSelect={selectFolder}
              />
            ) : null}
          </aside>

          <section className="content catalog-content">
            <div className="catalog-hero">
              <div>
                <p className="section-label">Biblioteca</p>
                <h2>{selectedLibrary ? selectedLibrary.name : 'Sua biblioteca'}</h2>
                <p>{folderPath ? formatLabel(folderPath) : 'Explore sua coleção por categorias e pastas.'}</p>
              </div>
              <div className="catalog-search">
                <input
                  value={query}
                  onChange={(event) => updateQuery(event.target.value)}
                  placeholder="Buscar livros, autores ou categorias..."
                  aria-label="Buscar livros, autores ou categorias"
                />
                <select
                  value={statusFilter}
                  onChange={(event) => updateFilter(event.target.value)}
                  aria-label="Filtrar por status"
                >
                  <option value="">Todos os status</option>
                  <option value="discovered">Descoberto</option>
                  <option value="changed">Alterado</option>
                  <option value="missing">Ausente</option>
                </select>
              </div>
            </div>

            {!booksLoading && books.length === 0 ? <div className="empty-state">Nenhum livro encontrado.</div> : null}

            {shelves.map((shelf) => (
              <section className="shelf" aria-label={`Livros em ${shelf.label}`} key={shelf.label}>
                <div className="shelf-heading">
                  <div>
                    <p className="section-label">Coleção</p>
                    <h2>{shelf.label}</h2>
                  </div>
                  <span>{shelf.books.length} livros</span>
                </div>

                <div className="book-grid">
                  {shelf.books.map((book) => (
                    <BookCard
                      book={book}
                      coverRefreshToken={coverRefreshToken}
                      key={book.id}
                      metadataLoading={metadataLoadingId === book.id}
                      onRead={onRead}
                      onRefreshMetadata={refreshMetadata}
                    />
                  ))}
                </div>
              </section>
            ))}

              <div className="pagination catalog-pagination">
                <button type="button" disabled={offset === 0} onClick={() => setOffset(offset - pageSize)}>
                  Anterior
                </button>
                <span>
                  {books.length === 0 ? '0' : `${offset + 1}-${offset + books.length}`}
                </span>
                <button
                  type="button"
                  disabled={books.length < pageSize}
                  onClick={() => setOffset(offset + pageSize)}
                >
                  Próxima
                </button>
              </div>
          </section>
        </section>
      ) : view === 'settings' ? (
        <section className="settings-layout">
          <div className="panel settings-panel">
            <div>
              <p className="section-label">Configuração da biblioteca</p>
              <h2>Adicionar biblioteca local</h2>
            </div>
            <form className="create-form settings-form" onSubmit={createLibrary}>
              <label>
                <span>Nome</span>
                <input
                  required
                  value={form.name}
                  onChange={(event) => setForm({ ...form, name: event.target.value })}
                  placeholder="Programação"
                />
              </label>
              <label>
                <span>Caminho</span>
                <input
                  required
                  value={form.path}
                  onChange={(event) => setForm({ ...form, path: event.target.value })}
                  placeholder="/books"
                />
              </label>
              <button type="submit" disabled={creating}>
                {creating ? 'Adicionando...' : 'Adicionar biblioteca'}
              </button>
            </form>
          </div>

          <div className="panel settings-panel">
            <div className="settings-header">
              <div>
                <p className="section-label">Bibliotecas</p>
                <h2>Bibliotecas cadastradas</h2>
              </div>
              {selectedLibrary ? <span className="muted">Selecionada: {selectedLibrary.name}</span> : null}
            </div>

            {scanSummary ? (
              <div className="summary-grid" aria-label="Resumo do último scan">
                <SummaryStat label="Novos" value={scanSummary.new} />
                <SummaryStat label="Alterados" value={scanSummary.changed} />
                <SummaryStat label="Sem alteração" value={scanSummary.unchanged} />
                <SummaryStat label="Ausentes" value={scanSummary.missing} />
              </div>
            ) : null}

            <div className="settings-library-list">
              {libraryLoading ? <p className="muted">Carregando bibliotecas...</p> : null}
              {!libraryLoading && libraries.length === 0 ? <p className="muted">Nenhuma biblioteca cadastrada.</p> : null}
              {libraries.map((library) => (
                <div className="settings-library" key={library.id}>
                  <button
                    className={library.id === selectedLibraryId ? 'library-item selected' : 'library-item'}
                    type="button"
                    onClick={() => {
                      setSelectedLibraryId(library.id);
                      setFolderPath('');
                      setOffset(0);
                      setScanSummary(null);
                    }}
                  >
                    <span>{library.name}</span>
                    <small>Atualizada em {formatDate(library.updated_at)}</small>
                  </button>
                  <div className="actions">
                    <button
                      type="button"
                      onClick={() => scanLibrary(library.id)}
                      disabled={scanningId === library.id}
                    >
                      {scanningId === library.id ? 'Escaneando...' : 'Escanear'}
                    </button>
                    <button
                      className="danger"
                      type="button"
                      onClick={() => deleteLibrary(library.id)}
                      disabled={scanningId === library.id}
                    >
                      Excluir
                    </button>
                  </div>
                </div>
              ))}
            </div>
          </div>
        </section>
      ) : (
        <MetadataJobsPanel selectedLibraryId={selectedLibraryId} selectedLibraryName={selectedLibrary?.name ?? ''} />
      )}
    </main>
  );
}

function MetadataJobsPanel({
  selectedLibraryId,
  selectedLibraryName,
}: {
  selectedLibraryId: string;
  selectedLibraryName: string;
}) {
  const [jobs, setJobs] = useState<MetadataJob[]>([]);
  const [status, setStatus] = useState('');
  const [loading, setLoading] = useState(false);
  const [queueing, setQueueing] = useState(false);
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');

  async function loadJobs() {
    setLoading(true);
    setError('');
    try {
      const params = new URLSearchParams({ limit: '100' });
      if (selectedLibraryId) {
        params.set('library_id', selectedLibraryId);
      }
      if (status) {
        params.set('status', status);
      }
      const response = await requestJSON<{ items: MetadataJob[] }>(`/api/metadata/jobs?${params}`);
      setJobs(response.items);
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setLoading(false);
    }
  }

  async function queueMissingMetadata() {
    setQueueing(true);
    setError('');
    setNotice('');
    try {
      const response = await requestJSON<{ queued: number }>('/api/metadata/jobs', {
        method: 'POST',
        body: JSON.stringify({
          library_id: selectedLibraryId,
          limit: 200,
        }),
      });
      setNotice(`${response.queued} jobs enfileirados.`);
      await loadJobs();
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setQueueing(false);
    }
  }

  useEffect(() => {
    loadJobs();
  }, [selectedLibraryId, status]);

  return (
    <section className="metadata-layout">
      {error ? (
        <div className="alert" role="alert">
          {error}
        </div>
      ) : null}
      {notice ? <div className="notice">{notice}</div> : null}

      <div className="panel metadata-panel">
        <div className="metadata-header">
          <div>
            <p className="section-label">Fila de metadados</p>
            <h2>{selectedLibraryName ? selectedLibraryName : 'Todas as bibliotecas'}</h2>
          </div>
          <div className="metadata-actions">
            <select value={status} onChange={(event) => setStatus(event.target.value)} aria-label="Filtrar jobs">
              <option value="">Todos os jobs</option>
              <option value="queued">Na fila</option>
              <option value="running">Executando</option>
              <option value="succeeded">Concluídos</option>
              <option value="failed">Com erro</option>
            </select>
            <button type="button" onClick={loadJobs} disabled={loading}>
              {loading ? 'Atualizando...' : 'Atualizar'}
            </button>
            <button type="button" onClick={queueMissingMetadata} disabled={queueing}>
              {queueing ? 'Enfileirando...' : 'Enfileirar pendentes'}
            </button>
          </div>
        </div>

        <p className="muted">
          O worker processa um livro por vez. Capas locais são extraídas para o cache quando possível.
        </p>

        <div className="jobs-list">
          {loading ? <p className="muted">Carregando jobs...</p> : null}
          {!loading && jobs.length === 0 ? <p className="muted">Nenhum job encontrado.</p> : null}
          {jobs.map((job) => (
            <article className="job-item" key={job.id}>
              <div>
                <strong>{job.filename || job.book_id}</strong>
                <span>{job.relative_path || job.book_id}</span>
                {job.last_error ? <small className="job-error">{job.last_error}</small> : null}
              </div>
              <div className="job-meta">
                <span className={`job-status ${job.status}`}>{metadataJobStatusLabel(job.status)}</span>
                <small>{job.attempts} tentativa(s)</small>
                <small>Atualizado em {formatDate(job.updated_at)}</small>
              </div>
            </article>
          ))}
        </div>
      </div>
    </section>
  );
}

function LibraryPicker({
  libraries,
  loading,
  selectedLibraryId,
  onSelect,
}: {
  libraries: Library[];
  loading: boolean;
  selectedLibraryId: string;
  onSelect: (libraryId: string) => void;
}) {
  return (
    <div className="library-list">
      <p className="section-label">Bibliotecas</p>
      {loading ? <p className="muted">Carregando bibliotecas...</p> : null}
      {!loading && libraries.length === 0 ? <p className="muted">Nenhuma biblioteca cadastrada.</p> : null}
      {libraries.map((library) => (
        <button
          className={library.id === selectedLibraryId ? 'library-item selected' : 'library-item'}
          key={library.id}
          type="button"
          onClick={() => onSelect(library.id)}
        >
          <span>{library.name}</span>
          <small>{formatDate(library.updated_at)}</small>
        </button>
      ))}
    </div>
  );
}

function FolderPicker({
  folders,
  folderPath,
  loading,
  onSelect,
}: {
  folders: Folder[];
  folderPath: string;
  loading: boolean;
  onSelect: (folderPath: string) => void;
}) {
  return (
    <div className="folder-panel">
      <div className="folder-heading">
        <span>Categorias</span>
        <small>{folderPath || 'Raiz'}</small>
      </div>
      <div className="folder-list">
        <button
          className={folderPath === '' ? 'folder-item selected' : 'folder-item'}
          type="button"
          onClick={() => onSelect('')}
        >
          <span>Todas as pastas</span>
        </button>
        {folderPath ? (
          <button className="folder-item" type="button" onClick={() => onSelect(parentFolder(folderPath))}>
            <span>Subir um nível</span>
          </button>
        ) : null}
        {loading ? <p className="muted">Carregando categorias...</p> : null}
        {!loading && folders.length === 0 ? <p className="muted">Nenhuma subpasta.</p> : null}
        {folders.map((folder) => (
          <button
            className={folder.path === folderPath ? 'folder-item selected' : 'folder-item'}
            key={folder.path}
            type="button"
            onClick={() => onSelect(folder.path)}
          >
            <span>{folder.name}</span>
            <small>{folder.book_count} livros</small>
          </button>
        ))}
      </div>
    </div>
  );
}

function BookCard({
  book,
  coverRefreshToken,
  metadataLoading,
  onRead,
  onRefreshMetadata,
}: {
  book: Book;
  coverRefreshToken: number;
  metadataLoading: boolean;
  onRead: (bookId: string) => void;
  onRefreshMetadata: (bookId: string) => void;
}) {
  const title = displayTitle(book);

  return (
    <article className={book.status === 'missing' ? 'book-card missing-book' : 'book-card'}>
      <button
        className="cover-button"
        type="button"
        onClick={() => onRead(book.id)}
        disabled={book.status === 'missing'}
        aria-label={`Abrir ${book.filename}`}
      >
        <BookCover book={book} coverRefreshToken={coverRefreshToken} />
      </button>
      <div className="book-card-body">
        <strong title={book.filename}>{title}</strong>
        <span>{book.folder ? formatLabel(book.folder) : 'Raiz'}</span>
        <div className="book-card-actions">
          {book.status === 'missing' ? <span className={`status ${book.status}`}>{statusLabel(book.status)}</span> : null}
          <button
            className="metadata-button"
            type="button"
            disabled={book.status === 'missing' || metadataLoading}
            onClick={() => onRefreshMetadata(book.id)}
          >
            {metadataLoading ? 'Na fila...' : 'Buscar dados'}
          </button>
        </div>
      </div>
    </article>
  );
}

function BookCover({
  book,
  coverRefreshToken,
  large = false,
}: {
  book: Book;
  coverRefreshToken: number;
  large?: boolean;
}) {
  const [coverUnavailable, setCoverUnavailable] = useState(false);
  const title = displayTitle(book);
  const category = formatLabel(book.category || book.extension.replace('.', ''));

  useEffect(() => {
    setCoverUnavailable(false);
  }, [book.id, coverRefreshToken]);

  return (
    <div className={`${large ? 'cover-art cover-large' : 'cover-art'} ${coverClass(book)}${coverUnavailable ? ' cover-placeholder' : ''}`}>
      {!coverUnavailable ? (
        <img
          alt=""
          className="cover-image"
          loading="lazy"
          onError={() => setCoverUnavailable(true)}
          src={`/api/books/${book.id}/cover?v=${coverRefreshToken}`}
        />
      ) : null}
      <span className="cover-category">{category}</span>
      <strong>{title}</strong>
      <span className="cover-format">{book.extension.replace('.', '').toUpperCase()}</span>
    </div>
  );
}

function BookDetail({
  bookId,
  onBack,
  onLogout,
  onRead,
  theme,
  onToggleTheme,
}: {
  bookId: string;
  onBack: () => void;
  onLogout?: () => void;
  onRead: (bookId: string) => void;
  theme: AppTheme;
  onToggleTheme: () => void;
}) {
  const [book, setBook] = useState<Book | null>(null);
  const [progress, setProgress] = useState<ReadingProgress | null>(null);
  const [metadata, setMetadata] = useState<BookMetadata | null>(null);
  const [annotations, setAnnotations] = useState<BookAnnotation[]>([]);
  const [coverRefreshToken, setCoverRefreshToken] = useState(0);
  const [metadataLoading, setMetadataLoading] = useState(false);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setError('');
    setNotice('');

    async function loadDetail() {
      try {
        const nextBook = await requestJSON<Book>(`/api/books/${bookId}`);
        if (cancelled) {
          return;
        }
        setBook(nextBook);

        const [nextProgress, nextMetadata, nextAnnotations] = await Promise.all([
          requestOptionalJSON<ReadingProgress>(`/api/books/${bookId}/progress`),
          requestOptionalJSON<BookMetadata>(`/api/books/${bookId}/metadata`),
          requestJSON<{ items: BookAnnotation[] }>(`/api/books/${bookId}/annotations`),
        ]);
        if (cancelled) {
          return;
        }
        setProgress(nextProgress);
        setMetadata(nextMetadata);
        setAnnotations(nextAnnotations.items);
      } catch (err) {
        if (!cancelled) {
          setError(errorMessage(err));
        }
      } finally {
        if (!cancelled) {
          setLoading(false);
        }
      }
    }

    loadDetail();
    return () => {
      cancelled = true;
    };
  }, [bookId]);

  async function refreshMetadata() {
    setMetadataLoading(true);
    setError('');
    setNotice('');
    try {
      await requestJSON<MetadataJob>(`/api/books/${bookId}/metadata/refresh`, { method: 'POST' });
      setNotice('Busca de metadados enfileirada.');
      window.setTimeout(() => setCoverRefreshToken((value) => value + 1), 8000);
      window.setTimeout(async () => {
        setCoverRefreshToken((value) => value + 1);
        const nextMetadata = await requestOptionalJSON<BookMetadata>(`/api/books/${bookId}/metadata`);
        setMetadata(nextMetadata);
      }, 35000);
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setMetadataLoading(false);
    }
  }

  if (loading || error || !book) {
    return (
      <main className="app-shell">
        <header className="topbar">
          <a className="brand" href="/" aria-label="Ir para o catálogo">
            <span className="brand-mark">A</span>
            <span className="brand-copy">
              <strong>Alexandria</strong>
              <small>Livro</small>
            </span>
          </a>
          <div className="topbar-actions">
            <button className="back-button" type="button" onClick={onBack}>Voltar</button>
            <ThemeToggle theme={theme} onToggleTheme={onToggleTheme} />
            {onLogout ? <button className="logout-button" type="button" onClick={onLogout}>Sair</button> : null}
          </div>
        </header>
        {error ? <div className="alert">{error}</div> : <div className="empty-state">Carregando livro...</div>}
      </main>
    );
  }

  const title = metadata?.title ?? displayTitle(book);
  const authors = metadata?.authors.length ? metadata.authors.join(', ') : 'Autor desconhecido';
  const stats = readingStats(book, progress);

  return (
    <main className="app-shell">
      <header className="topbar">
        <a className="brand" href="/" aria-label="Ir para o catálogo">
          <span className="brand-mark">A</span>
          <span className="brand-copy">
            <strong>Alexandria</strong>
            <small>Livro</small>
          </span>
        </a>
        <div className="topbar-actions">
          <button className="back-button" type="button" onClick={onBack}>Voltar</button>
          <ThemeToggle theme={theme} onToggleTheme={onToggleTheme} />
          {onLogout ? <button className="logout-button" type="button" onClick={onLogout}>Sair</button> : null}
        </div>
      </header>

      {error ? <div className="alert">{error}</div> : null}
      {notice ? <div className="notice">{notice}</div> : null}

      <section className="book-detail">
        <aside className="detail-cover">
          <BookCover book={book} coverRefreshToken={coverRefreshToken} large />
        </aside>

        <section className="detail-main">
          <div className="detail-heading">
            <div>
              <p className="section-label">{formatLabel(book.category || book.extension.replace('.', ''))}</p>
              <h2>{title}</h2>
              <p>{authors}</p>
              {metadata?.description ? <p className="detail-description">{metadata.description}</p> : null}
            </div>
            <div className="detail-actions">
              <button type="button" onClick={() => onRead(book.id)} disabled={book.status === 'missing'}>
                Ler
              </button>
              <button type="button" onClick={refreshMetadata} disabled={metadataLoading}>
                {metadataLoading ? 'Na fila...' : 'Buscar dados'}
              </button>
            </div>
          </div>

          <div className="progress-panel">
            <div>
              <span>{stats.label}</span>
              <strong>{stats.primary}</strong>
            </div>
            <div>
              <span>Tempo restante</span>
              <strong>{stats.timeLeft}</strong>
            </div>
            <div>
              <span>Restante</span>
              <strong>{stats.remaining}</strong>
            </div>
          </div>

          <div className="progress-track" aria-label="Progresso de leitura">
            <span style={{ width: `${stats.percent}%` }} />
          </div>

          <dl className="detail-stats">
            <div>
              <dt>Status</dt>
              <dd>
                <span className={`status ${book.status}`}>{statusLabel(book.status)}</span>
              </dd>
            </div>
            <div>
              <dt>Formato</dt>
              <dd>{book.extension.replace('.', '').toUpperCase()}</dd>
            </div>
            <div>
              <dt>Tamanho</dt>
              <dd>{formatBytes(book.file_size)}</dd>
            </div>
            <div>
              <dt>Pasta</dt>
              <dd>{book.folder ? formatLabel(book.folder) : 'Raiz'}</dd>
            </div>
            <div>
              <dt>Modificado</dt>
              <dd>{formatDate(book.file_modified_at)}</dd>
            </div>
            <div>
              <dt>Metadados</dt>
              <dd>{metadata ? `${metadata.provider} · ${Math.round(metadata.confidence * 100)}%` : 'Não buscado'}</dd>
            </div>
          </dl>

          <div className="detail-path">{book.relative_path}</div>

          <section className="annotations-panel">
            <div className="annotations-heading">
              <div>
                <p className="section-label">Notas e marcações</p>
                <h2>Arquivo de estudo</h2>
              </div>
              <a href="/api/annotations/export">Baixar Markdown</a>
            </div>
            {annotations.length === 0 ? (
              <p className="muted">Nenhuma anotação salva ainda.</p>
            ) : (
              <div className="annotation-list">
                {annotations.slice(0, 8).map((annotation) => (
                  <article className="annotation-item" key={annotation.id}>
                    <div>
                      <strong>{annotationKindLabel(annotation.kind)}</strong>
                      <span>{annotationLocationLabel(annotation)}</span>
                    </div>
                    {annotation.note ? <p>{annotation.note}</p> : null}
                    <small>{formatDate(annotation.created_at)}</small>
                  </article>
                ))}
              </div>
            )}
          </section>
        </section>
      </section>
    </main>
  );
}

function Reader({ bookId, onBack }: { bookId: string; onBack: () => void }) {
  const [book, setBook] = useState<Book | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setError('');

    requestJSON<Book>(`/api/books/${bookId}`)
      .then((nextBook) => {
        if (!cancelled) {
          setBook(nextBook);
        }
      })
      .catch((err) => {
        if (!cancelled) {
          setError(errorMessage(err));
        }
      })
      .finally(() => {
        if (!cancelled) {
          setLoading(false);
        }
      });

    return () => {
      cancelled = true;
    };
  }, [bookId]);

  if (loading || error || !book) {
    return (
      <ReaderFrame onBack={onBack} title="Leitor" subtitle="">
        {error ? (
          <div className="reader-alert" role="alert">
            {error}
          </div>
        ) : (
          <p className="reader-state">Carregando livro...</p>
        )}
      </ReaderFrame>
    );
  }

  if (book.extension.toLowerCase() === '.pdf') {
    return <PDFReader book={book} onBack={onBack} />;
  }

  return <EBookReader book={book} onBack={onBack} />;
}

function PDFReader({ book, onBack }: { book: Book; onBack: () => void }) {
  const [pdf, setPDF] = useState<PDFDocumentProxy | null>(null);
  const [pageNumber, setPageNumber] = useState(1);
  const [totalPages, setTotalPages] = useState(0);
  const [settings, setSettings] = useStoredSettings<PDFSettings>(pdfSettingsKey, defaultPDFSettings);
  const [containerWidth, setContainerWidth] = useState(0);
  const [loading, setLoading] = useState(true);
  const [rendering, setRendering] = useState(false);
  const [error, setError] = useState('');
  const [annotationNote, setAnnotationNote] = useState('');
  const [showAnnotationPanel, setShowAnnotationPanel] = useState(false);
  const [annotationSaving, setAnnotationSaving] = useState(false);
  const [annotationNotice, setAnnotationNotice] = useState('');
  const [controlsVisible, setControlsVisible] = useState(true);
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [moreOpen, setMoreOpen] = useState(false);
  const canvasRef = useRef<HTMLCanvasElement | null>(null);
  const pageShellRef = useRef<HTMLDivElement | null>(null);
  const controlsTimerRef = useRef<number | null>(null);
  const touchStartRef = useRef<{ x: number; y: number; time: number } | null>(null);
  const lastTouchAtRef = useRef(0);
  const renderQueueRef = useRef(Promise.resolve());
  const renderGenerationRef = useRef(0);

  useEffect(() => {
    if (controlsTimerRef.current !== null) {
      window.clearTimeout(controlsTimerRef.current);
      controlsTimerRef.current = null;
    }

    if (controlsVisible && !settingsOpen && !moreOpen) {
      controlsTimerRef.current = window.setTimeout(() => setControlsVisible(false), 3200);
    }

    return () => {
      if (controlsTimerRef.current !== null) {
        window.clearTimeout(controlsTimerRef.current);
        controlsTimerRef.current = null;
      }
    };
  }, [controlsVisible, settingsOpen, moreOpen]);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setError('');

    async function loadReader() {
      try {
        const progress = await requestJSON<ReadingProgress>(`/api/books/${book.id}/progress`);
        if (cancelled) {
          return;
        }

        const pdfjs = await loadPDFJS();
        const loaded = await pdfjs.getDocument({ url: `/api/books/${book.id}/file` }).promise;
        if (cancelled) {
          return;
        }
        setPDF(loaded);
        setTotalPages(loaded.numPages);
        setPageNumber(clampPage(progress.current_page, loaded.numPages));
      } catch (err) {
        if (!cancelled) {
          setError(errorMessage(err));
        }
      } finally {
        if (!cancelled) {
          setLoading(false);
        }
      }
    }

    loadReader();
    return () => {
      cancelled = true;
    };
  }, [book.id]);

  useEffect(() => {
    const node = pageShellRef.current;
    if (!node) {
      return;
    }

    function updateWidth() {
      setContainerWidth(node?.clientWidth ?? 0);
    }

    updateWidth();
    const observer = new ResizeObserver(updateWidth);
    observer.observe(node);
    return () => observer.disconnect();
  }, []);

  useEffect(() => {
    const pageShell = pageShellRef.current;
    if (!pdf || !canvasRef.current || !pageShell || containerWidth === 0) {
      return;
    }

    let cancelled = false;
    let renderTask: PDFRenderTask | null = null;
    let releaseRender: (() => void) | null = null;
    const generation = ++renderGenerationRef.current;
    const previousRender = renderQueueRef.current;
    const currentRender = new Promise<void>((resolve) => {
      releaseRender = resolve;
    });
    renderQueueRef.current = currentRender;
    const document = pdf;
    setRendering(true);

    async function renderPage() {
      try {
        await previousRender;
        if (cancelled || !canvasRef.current) {
          return;
        }

        const page = await document.getPage(pageNumber);
        if (cancelled || !canvasRef.current) {
          return;
        }

        const baseViewport = page.getViewport({ scale: 1 });
        const currentPageShell = pageShellRef.current;
        if (!currentPageShell) {
          return;
        }
        const pageShellStyles = window.getComputedStyle(currentPageShell);
        const horizontalPadding =
          Number.parseFloat(pageShellStyles.paddingLeft) + Number.parseFloat(pageShellStyles.paddingRight);
        const fitWidth = Math.min(Math.max(containerWidth - horizontalPadding, 1), settings.maxWidth);
        const fitScale = fitWidth / baseViewport.width;
        const viewport = page.getViewport({ scale: fitScale * settings.zoom });
        const pixelRatio = window.devicePixelRatio || 1;
        const canvas = canvasRef.current;
        const context = canvas.getContext('2d');
        if (!context) {
          return;
        }

        canvas.width = Math.floor(viewport.width * pixelRatio);
        canvas.height = Math.floor(viewport.height * pixelRatio);
        canvas.style.width = `${Math.floor(viewport.width)}px`;
        canvas.style.height = `${Math.floor(viewport.height)}px`;

        renderTask = page.render({
          canvas,
          canvasContext: context,
          viewport,
          transform: pixelRatio === 1 ? undefined : [pixelRatio, 0, 0, pixelRatio, 0, 0],
        });
        await renderTask.promise;
      } finally {
        releaseRender?.();
      }
    }

    renderPage()
      .catch((err) => {
        if (!cancelled && generation === renderGenerationRef.current && err instanceof Error && err.name !== 'RenderingCancelledException') {
          setError(errorMessage(err));
        }
      })
      .finally(() => {
        if (!cancelled && generation === renderGenerationRef.current) {
          setRendering(false);
        }
      });

    return () => {
      cancelled = true;
      renderTask?.cancel();
    };
  }, [pdf, pageNumber, settings, containerWidth]);

  useEffect(() => {
    if (!totalPages) {
      return;
    }

    const timeout = window.setTimeout(() => {
      requestJSON<ReadingProgress>(`/api/books/${book.id}/progress`, {
        method: 'PUT',
        body: JSON.stringify({
          current_page: pageNumber,
          total_pages: totalPages,
        }),
      }).catch(() => undefined);
    }, 350);

    return () => window.clearTimeout(timeout);
  }, [book.id, pageNumber, totalPages]);

  function goToPage(nextPage: number) {
    setPageNumber(clampPage(nextPage, totalPages || 1));
    pageShellRef.current?.scrollTo({ top: 0, left: 0 });
  }

  function navigateFromPosition(clientX: number) {
    const node = pageShellRef.current;
    if (!node) {
      return;
    }

    const position = (clientX - node.getBoundingClientRect().left) / node.clientWidth;
    if (position < 0.25) {
      goToPage(pageNumber - 1);
    } else if (position > 0.75) {
      goToPage(pageNumber + 1);
    } else {
      setControlsVisible((visible) => !visible);
    }
  }

  function handlePageTouchStart(event: ReactTouchEvent<HTMLDivElement>) {
    const touch = event.changedTouches[0];
    if (touch) {
      touchStartRef.current = { x: touch.clientX, y: touch.clientY, time: event.timeStamp };
    }
  }

  function handlePageTouchEnd(event: ReactTouchEvent<HTMLDivElement>) {
    const touch = event.changedTouches[0];
    const start = touchStartRef.current;
    touchStartRef.current = null;
    if (!touch || !start) {
      return;
    }

    const deltaX = touch.clientX - start.x;
    const deltaY = touch.clientY - start.y;
    const elapsed = event.timeStamp - start.time;
    if (Math.abs(deltaX) > 48 && Math.abs(deltaX) > Math.abs(deltaY)) {
      goToPage(deltaX < 0 ? pageNumber + 1 : pageNumber - 1);
      return;
    }

    if (elapsed <= 600 && Math.hypot(deltaX, deltaY) <= 18) {
      lastTouchAtRef.current = Date.now();
      navigateFromPosition(touch.clientX);
    }
  }

  function handlePageClick(event: ReactMouseEvent<HTMLDivElement>) {
    if (Date.now() - lastTouchAtRef.current < 600) {
      return;
    }
    navigateFromPosition(event.clientX);
  }

  async function saveAnnotation(kind: BookAnnotation['kind'], note = '') {
    setAnnotationSaving(true);
    setAnnotationNotice('');
    setError('');
    try {
      await requestJSON<BookAnnotation>(`/api/books/${book.id}/annotations`, {
        method: 'POST',
        body: JSON.stringify({
          kind,
          page_number: pageNumber,
          total_pages: totalPages || null,
          note,
        }),
      });
      setAnnotationNotice(kind === 'note' ? 'Nota salva no arquivo de estudo.' : 'Página marcada.');
      if (kind === 'note') {
        setAnnotationNote('');
        setShowAnnotationPanel(false);
      }
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setAnnotationSaving(false);
    }
  }

  function toggleSettingsPanel() {
    setSettingsOpen((open) => !open);
    setControlsVisible(true);
    setMoreOpen(false);
  }

  const canAnnotate = !annotationSaving && !loading && !error && totalPages > 0;
  const progress = totalPages > 1 ? (pageNumber - 1) / (totalPages - 1) : 0;

  return (
    <ReaderFrame
      onBack={onBack}
      title={book.filename}
      subtitle={`Página ${pageNumber}${totalPages ? ` de ${totalPages}` : ''}`}
      theme={settings.theme}
      immersive
      controlsVisible={controlsVisible}
      settingsOpen={settingsOpen}
      onToggleSettings={toggleSettingsPanel}
      progress={progress}
      actions={
        <button
          type="button"
          className="reader-icon-button"
          aria-label="Mais opções"
          onClick={() => {
            setMoreOpen((open) => !open);
            setSettingsOpen(false);
            setControlsVisible(true);
          }}
        >
          ⋮
        </button>
      }
      settingsPanel={
        <ReaderSettingsPanel>
          <label>
            <span>Zoom {Math.round(settings.zoom * 100)}%</span>
            <input
              min="70"
              max="240"
              step="5"
              type="range"
              value={Math.round(settings.zoom * 100)}
              onChange={(event) =>
                setSettings({ ...settings, zoom: clampNumber(Number(event.target.value) / 100, 0.7, 2.4) })
              }
            />
          </label>
          <label>
            <span>Largura da página {settings.maxWidth}px</span>
            <input
              min="360"
              max="1400"
              step="20"
              type="range"
              value={settings.maxWidth}
              onChange={(event) =>
                setSettings({ ...settings, maxWidth: clampNumber(Number(event.target.value), 360, 1400) })
              }
            />
          </label>
          <label>
            <span>Tema</span>
            <select
              value={settings.theme}
              onChange={(event) => setSettings({ ...settings, theme: event.target.value as ReaderTheme })}
            >
              <option value="dark">Escuro</option>
              <option value="light">Claro</option>
              <option value="sepia">Sépia</option>
            </select>
          </label>
          <button type="button" onClick={() => setSettings(defaultPDFSettings)}>
            Redefinir
          </button>
        </ReaderSettingsPanel>
      }
      bottomBar={
        <>
          <button type="button" aria-label="Aparência" onClick={toggleSettingsPanel}>
            Aa
          </button>
          <button type="button" aria-label="Página anterior" disabled={pageNumber <= 1} onClick={() => goToPage(pageNumber - 1)}>
            ‹
          </button>
          <span className="reader-page-indicator">
            {pageNumber} / {totalPages || '—'}
          </span>
          <button type="button" aria-label="Próxima página" disabled={!totalPages || pageNumber >= totalPages} onClick={() => goToPage(pageNumber + 1)}>
            ›
          </button>
          <button type="button" aria-label="Marcar página" disabled={!canAnnotate} onClick={() => saveAnnotation('bookmark')}>
            🔖
          </button>
          <button
            type="button"
            aria-label="Mais opções"
            onClick={() => {
              setMoreOpen((open) => !open);
              setSettingsOpen(false);
              setControlsVisible(true);
            }}
          >
            ⋮
          </button>
        </>
      }
    >
      <div className="reader-content-stage">
        {error ? (
          <div className="reader-alert" role="alert">
            {error}
          </div>
        ) : null}
        {annotationNotice ? <div className="reader-notice">{annotationNotice}</div> : null}
        {showAnnotationPanel ? (
          <AnnotationEditor
            disabled={!canAnnotate}
            note={annotationNote}
            onCancel={() => setShowAnnotationPanel(false)}
            onChange={setAnnotationNote}
            onSave={() => saveAnnotation('note', annotationNote)}
            placeholder={`Nota da página ${pageNumber}`}
          />
        ) : null}
        {moreOpen ? (
          <div className="reader-more-menu">
            <button type="button" disabled={!canAnnotate} onClick={() => { setMoreOpen(false); void saveAnnotation('bookmark'); }}>
              🔖 Marcar página
            </button>
            <button type="button" onClick={() => { setMoreOpen(false); setShowAnnotationPanel(true); }}>
              Nota
            </button>
            <label className="reader-page-jump">
              <span>Ir para página</span>
              <input
                aria-label="Ir para página"
                min={1}
                max={totalPages || 1}
                type="number"
                value={pageNumber}
                onChange={(event) => goToPage(Number(event.target.value))}
              />
            </label>
          </div>
        ) : null}
        <div
          className="reader-page-shell pdf-reader-page"
          ref={pageShellRef}
          onClick={handlePageClick}
          onTouchStart={handlePageTouchStart}
          onTouchEnd={handlePageTouchEnd}
        >
          {loading ? <p className="reader-state">Carregando PDF...</p> : null}
          {!loading && !error ? (
            <canvas className={rendering ? 'pdf-page rendering' : 'pdf-page'} ref={canvasRef} />
          ) : null}
        </div>
      </div>
    </ReaderFrame>
  );
}

function EBookReader({ book, onBack }: { book: Book; onBack: () => void }) {
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [fraction, setFraction] = useState<number | null>(null);
  const [locator, setLocator] = useState<string | null>(null);
  const [annotationNote, setAnnotationNote] = useState('');
  const [showAnnotationPanel, setShowAnnotationPanel] = useState(false);
  const [annotationSaving, setAnnotationSaving] = useState(false);
  const [annotationNotice, setAnnotationNotice] = useState('');
  const [settings, setSettings] = useStoredSettings<EBookSettings>(ebookSettingsKey, defaultEBookSettings);
  const [controlsVisible, setControlsVisible] = useState(true);
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [tocOpen, setTocOpen] = useState(false);
  const [moreOpen, setMoreOpen] = useState(false);
  const [toc, setToc] = useState<ReaderTOCItem[]>([]);
  const viewRef = useRef<FoliateViewElement | null>(null);
  const settingsRef = useRef(settings);
  const controlsTimerRef = useRef<number | null>(null);

  useEffect(() => {
    settingsRef.current = settings;
    if (viewRef.current) {
      applyEBookSettings(viewRef.current, settings);
    }
  }, [settings]);

  useEffect(() => {
    if (controlsTimerRef.current !== null) {
      window.clearTimeout(controlsTimerRef.current);
      controlsTimerRef.current = null;
    }

    if (controlsVisible && !settingsOpen && !tocOpen && !moreOpen) {
      controlsTimerRef.current = window.setTimeout(() => setControlsVisible(false), 3200);
    }

    return () => {
      if (controlsTimerRef.current !== null) {
        window.clearTimeout(controlsTimerRef.current);
        controlsTimerRef.current = null;
      }
    };
  }, [controlsVisible, settingsOpen, tocOpen, moreOpen]);

  useEffect(() => {
    let cancelled = false;
    const view = document.createElement('foliate-view') as FoliateViewElement;
    viewRef.current = view;

    function onSectionLoad(event: Event) {
      const { doc } = (event as CustomEvent<{ doc: Document }>).detail;
      let touchStart: { x: number; y: number; time: number } | null = null;
      let lastTouchAt = 0;

      function navigateFromPosition(clientX: number) {
        const width = doc.documentElement.clientWidth || window.innerWidth;
        const position = clientX / width;
        if (position < 0.25) {
          void view.prev();
        } else if (position > 0.75) {
          void view.next();
        } else {
          setControlsVisible((visible) => !visible);
        }
      }

      function canHandleTap(target: EventTarget | null) {
        return !(target instanceof Element && target.closest('a, button, input, select, textarea'));
      }

      function hasSelection() {
        const selection = doc.getSelection();
        return Boolean(selection && !selection.isCollapsed);
      }

      doc.addEventListener('touchstart', (touchEvent) => {
        const touch = touchEvent.changedTouches[0];
        if (touch) {
          touchStart = { x: touch.clientX, y: touch.clientY, time: touchEvent.timeStamp };
        }
      }, { passive: true });

      doc.addEventListener('touchend', (touchEvent) => {
        const touch = touchEvent.changedTouches[0];
        if (!touch || !touchStart || !canHandleTap(touchEvent.target) || hasSelection()) {
          touchStart = null;
          return;
        }

        const elapsed = touchEvent.timeStamp - touchStart.time;
        const distance = Math.hypot(touch.clientX - touchStart.x, touch.clientY - touchStart.y);
        touchStart = null;
        if (elapsed <= 600 && distance <= 18) {
          lastTouchAt = Date.now();
          navigateFromPosition(touch.clientX);
        }
      }, { passive: true });

      doc.addEventListener('click', (clickEvent) => {
        if (clickEvent.defaultPrevented || clickEvent.button !== 0) {
          return;
        }

        if (Date.now() - lastTouchAt < 600 || !canHandleTap(clickEvent.target) || hasSelection()) {
          return;
        }

        navigateFromPosition(clickEvent.clientX);
      });
    }

    async function loadReader() {
      setLoading(true);
      setError('');
      try {
        await import('foliate-js/view.js');
        const progress = await requestJSON<ReadingProgress>(`/api/books/${book.id}/progress`);
        if (cancelled) {
          return;
        }
        setLocator(progress.locator);
        setFraction(progress.fraction);

        const target = document.getElementById('ebook-view');
        if (!target) {
          throw new Error('Contêiner do leitor indisponível');
        }
        target.replaceChildren(view);

        view.addEventListener('load', onSectionLoad as EventListener);
        view.addEventListener('relocate', onRelocate as EventListener);
        await view.open(`/api/books/${book.id}/file`);
        setToc((view.book?.toc ?? []) as ReaderTOCItem[]);
        applyEBookSettings(view, settingsRef.current);
        if (progress.locator) {
          await view.goTo(progress.locator);
        } else {
          await view.init({ showTextStart: true });
        }
      } catch (err) {
        if (!cancelled) {
          setError(errorMessage(err));
        }
      } finally {
        if (!cancelled) {
          setLoading(false);
        }
      }
    }

    function onRelocate(event: CustomEvent<FoliateRelocateDetail>) {
      const detail = event.detail;
      const locator = typeof detail.cfi === 'string' ? detail.cfi : undefined;
      const nextFraction = typeof detail.fraction === 'number' ? detail.fraction : null;
      setFraction(nextFraction);
      setLocator(locator ?? null);
      if (!locator) {
        return;
      }

      requestJSON<ReadingProgress>(`/api/books/${book.id}/progress`, {
        method: 'PUT',
        body: JSON.stringify({
          current_page: 1,
          locator,
          fraction: nextFraction,
        }),
      }).catch(() => undefined);
    }

    loadReader();
    return () => {
      cancelled = true;
      view.removeEventListener('load', onSectionLoad as EventListener);
      view.removeEventListener('relocate', onRelocate as EventListener);
      view.close?.();
      view.remove();
    };
  }, [book.id]);

  async function saveAnnotation(kind: BookAnnotation['kind'], note = '') {
    setAnnotationSaving(true);
    setAnnotationNotice('');
    setError('');
    try {
      await requestJSON<BookAnnotation>(`/api/books/${book.id}/annotations`, {
        method: 'POST',
        body: JSON.stringify({
          kind,
          locator,
          fraction: fraction ?? 0,
          note,
        }),
      });
      setAnnotationNotice(kind === 'note' ? 'Nota salva no arquivo de estudo.' : 'Trecho marcado.');
      if (kind === 'note') {
        setAnnotationNote('');
        setShowAnnotationPanel(false);
      }
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setAnnotationSaving(false);
    }
  }

  function toggleSettingsPanel() {
    setSettingsOpen((open) => !open);
    setControlsVisible(true);
    setTocOpen(false);
    setMoreOpen(false);
  }

  const canAnnotate = !annotationSaving && !loading && !error;

  return (
    <ReaderFrame
      onBack={onBack}
      title={book.filename}
      subtitle={fraction == null ? 'EPUB/MOBI' : `${Math.round(fraction * 100)}%`}
      theme={settings.theme}
      immersive
      controlsVisible={controlsVisible}
      settingsOpen={settingsOpen}
      onToggleSettings={toggleSettingsPanel}
      progress={fraction}
      actions={
        <button
          type="button"
          className="reader-icon-button"
          aria-label="Mais opções"
          onClick={() => {
            setMoreOpen((open) => !open);
            setControlsVisible(true);
            setSettingsOpen(false);
            setTocOpen(false);
          }}
        >
          ⋮
        </button>
      }
      settingsPanel={
        <ReaderSettingsPanel>
          <label>
            <span>Tamanho da fonte</span>
            <div className="reader-stepper">
              <button
                type="button"
                onClick={() => setSettings({ ...settings, zoom: clampNumber(settings.zoom - 0.05, 0.85, 1.5) })}
              >
                −
              </button>
              <output>{Math.round(16 * settings.zoom)}px</output>
              <button
                type="button"
                onClick={() => setSettings({ ...settings, zoom: clampNumber(settings.zoom + 0.05, 0.85, 1.5) })}
              >
                +
              </button>
            </div>
          </label>
          <label>
            <span>Fonte</span>
            <select
              value={settings.fontFamily}
              onChange={(event) => setSettings({ ...settings, fontFamily: event.target.value as EBookSettings['fontFamily'] })}
            >
              <option value="serif">Serifada</option>
              <option value="sans">Sem serifa</option>
            </select>
          </label>
          <label>
            <span>Espaçamento {settings.lineHeight.toFixed(2)}</span>
            <input
              min="1.5"
              max="2"
              step="0.05"
              type="range"
              value={settings.lineHeight}
              onChange={(event) => setSettings({ ...settings, lineHeight: Number(event.target.value) })}
            />
          </label>
          <label>
            <span>Largura do texto {settings.maxWidth}px</span>
            <input
              min="320"
              max="900"
              step="20"
              type="range"
              value={settings.maxWidth}
              onChange={(event) => setSettings({ ...settings, maxWidth: clampNumber(Number(event.target.value), 320, 900) })}
            />
          </label>
          <label>
            <span>Modo</span>
            <select
              value={settings.flow}
              onChange={(event) => setSettings({ ...settings, flow: event.target.value as EBookFlow })}
            >
              <option value="paginated">Paginado</option>
              <option value="scrolled">Rolagem</option>
            </select>
          </label>
          <label>
            <span>Tema</span>
            <select
              value={settings.theme}
              onChange={(event) => setSettings({ ...settings, theme: event.target.value as ReaderTheme })}
            >
              <option value="dark">Escuro</option>
              <option value="light">Claro</option>
              <option value="sepia">Sépia</option>
            </select>
          </label>
          <button type="button" onClick={() => setSettings(defaultEBookSettings)}>
            Redefinir
          </button>
        </ReaderSettingsPanel>
      }
      bottomBar={
        <>
          <button type="button" aria-label="Aparência" onClick={toggleSettingsPanel}>
            Aa
          </button>
          <button
            type="button"
            aria-label="Sumário"
            onClick={() => {
              setTocOpen((open) => !open);
              setSettingsOpen(false);
              setMoreOpen(false);
              setControlsVisible(true);
            }}
          >
            ☰
          </button>
          <button
            type="button"
            aria-label="Marcar página"
            disabled={!canAnnotate}
            onClick={() => saveAnnotation('bookmark')}
          >
            🔖
          </button>
          <button
            type="button"
            aria-label="Mais opções"
            onClick={() => {
              setMoreOpen((open) => !open);
              setSettingsOpen(false);
              setTocOpen(false);
              setControlsVisible(true);
            }}
          >
            ⋮
          </button>
        </>
      }
    >
      <div className="reader-content-stage">
        {error ? (
          <div className="reader-alert" role="alert">
            {error}
          </div>
        ) : null}
        {annotationNotice ? <div className="reader-notice">{annotationNotice}</div> : null}
        {showAnnotationPanel ? (
          <AnnotationEditor
            disabled={!canAnnotate}
            note={annotationNote}
            onCancel={() => setShowAnnotationPanel(false)}
            onChange={setAnnotationNote}
            onSave={() => saveAnnotation('note', annotationNote)}
            placeholder="Nota deste trecho"
          />
        ) : null}
        {moreOpen ? (
          <div className="reader-more-menu">
            <button
              type="button"
              disabled={!canAnnotate}
              onClick={() => {
                setMoreOpen(false);
                saveAnnotation('bookmark');
              }}
            >
              🔖 Marcar página
            </button>
            <button
              type="button"
              onClick={() => {
                setMoreOpen(false);
                setShowAnnotationPanel(true);
              }}
            >
              Nota
            </button>
          </div>
        ) : null}
        {tocOpen ? (
          <ReaderTOCSheet
            items={toc}
            onClose={() => setTocOpen(false)}
            onSelect={(href) => {
              void viewRef.current?.goTo(href);
              setTocOpen(false);
              setControlsVisible(true);
            }}
          />
        ) : null}
        {loading ? <p className="reader-state">Carregando ebook...</p> : null}
        <div id="ebook-view" className="ebook-view" />
      </div>
    </ReaderFrame>
  );
}

function ReaderTOCSheet({
  items,
  onClose,
  onSelect,
}: {
  items: ReaderTOCItem[];
  onClose: () => void;
  onSelect: (href: string) => void;
}) {
  return (
    <aside className="reader-toc-sheet" aria-label="Sumário">
      <div className="reader-sheet-header">
        <strong>Sumário</strong>
        <button type="button" onClick={onClose}>
          Fechar
        </button>
      </div>
      {items.length ? (
        <nav>
          <ReaderTOCItems items={items} onSelect={onSelect} />
        </nav>
      ) : (
        <p className="reader-state">Este livro não possui sumário.</p>
      )}
    </aside>
  );
}

function ReaderTOCItems({ items, onSelect }: { items: ReaderTOCItem[]; onSelect: (href: string) => void }) {
  return (
    <ul>
      {items.map((item) => (
        <li key={`${item.href}-${item.label}`}>
          <button type="button" onClick={() => onSelect(item.href)}>
            {item.label}
          </button>
          {item.subitems?.length ? <ReaderTOCItems items={item.subitems} onSelect={onSelect} /> : null}
        </li>
      ))}
    </ul>
  );
}

function ReaderFrame({
  onBack,
  title,
  subtitle,
  actions,
  settingsPanel,
  immersive = false,
  controlsVisible = true,
  settingsOpen,
  onToggleSettings,
  progress,
  bottomBar,
  theme = 'dark',
  children,
}: {
  onBack: () => void;
  title: string;
  subtitle: string;
  actions?: ReactNode;
  settingsPanel?: ReactNode;
  immersive?: boolean;
  controlsVisible?: boolean;
  settingsOpen?: boolean;
  onToggleSettings?: () => void;
  progress?: number | null;
  bottomBar?: ReactNode;
  theme?: ReaderTheme;
  children: ReactNode;
}) {
  const [localSettingsOpen, setLocalSettingsOpen] = useState(false);
  const showSettings = settingsOpen ?? localSettingsOpen;
  const toggleSettings = onToggleSettings ?? (() => setLocalSettingsOpen((open) => !open));

  return (
    <main
      className={`reader-shell ${theme} ${immersive ? 'immersive-reader' : ''} ${
        controlsVisible ? 'reader-controls-visible' : 'reader-controls-hidden'
      }`}
    >
      <header className="reader-toolbar reader-chrome">
        <button type="button" className="reader-back-button" aria-label="Voltar" onClick={onBack}>
          {immersive ? '←' : 'Voltar'}
        </button>
        <div className="reader-title">
          <strong>{title}</strong>
          <span>{subtitle}</span>
        </div>
        <div className="reader-actions">
          {actions}
          {settingsPanel && !immersive ? (
            <button type="button" onClick={toggleSettings}>
              Ajustes
            </button>
          ) : null}
        </div>
      </header>
      {immersive && progress != null ? (
        <div className="reader-progress reader-chrome" aria-label={`Progresso ${Math.round(progress * 100)}%`}>
          <span style={{ width: `${Math.min(Math.max(progress, 0), 1) * 100}%` }} />
        </div>
      ) : null}
      {showSettings && settingsPanel ? (
        <div className={`reader-settings ${immersive ? 'reader-settings-sheet' : ''}`}>
          {immersive ? <div className="reader-sheet-header"><strong>Aparência</strong><button type="button" onClick={toggleSettings}>Fechar</button></div> : null}
          {settingsPanel}
        </div>
      ) : null}
      {children}
      {immersive && bottomBar ? <footer className="reader-bottom-bar reader-chrome">{bottomBar}</footer> : null}
    </main>
  );
}

function ReaderSettingsPanel({ children }: { children: ReactNode }) {
  return <div className="reader-settings-grid">{children}</div>;
}

function AnnotationEditor({
  disabled,
  note,
  onCancel,
  onChange,
  onSave,
  placeholder,
}: {
  disabled: boolean;
  note: string;
  onCancel: () => void;
  onChange: (note: string) => void;
  onSave: () => void;
  placeholder: string;
}) {
  return (
    <div className="annotation-editor">
      <textarea
        value={note}
        onChange={(event) => onChange(event.target.value)}
        placeholder={placeholder}
        rows={3}
      />
      <div>
        <button type="button" onClick={onSave} disabled={disabled || note.trim() === ''}>
          Salvar nota
        </button>
        <button type="button" onClick={onCancel}>
          Cancelar
        </button>
      </div>
    </div>
  );
}

function SummaryStat({ label, value }: { label: string; value: number }) {
  return (
    <div className="summary-stat">
      <span>{label}</span>
      <strong>{value}</strong>
    </div>
  );
}

async function requestJSON<T>(path: string, init: RequestInit = {}): Promise<T> {
  const response = await fetch(path, {
    ...init,
    headers: {
      'Content-Type': 'application/json',
      ...init.headers,
    },
  });

  if (response.status === 204) {
    return undefined as T;
  }


  const data = (await response.json()) as T | ApiError;
  if (!response.ok) {
    const apiError = data as ApiError;
    throw new Error(apiError.error?.message ?? 'Falha na requisição');
  }
  return data as T;
}

async function requestOptionalJSON<T>(path: string): Promise<T | null> {
  const response = await fetch(path, {
    headers: {
      'Content-Type': 'application/json',
    },
  });
  if (response.status === 404) {
    return null;
  }
  const data = (await response.json()) as T | ApiError;
  if (!response.ok) {
    const apiError = data as ApiError;
    throw new Error(apiError.error?.message ?? 'Falha na requisição');
  }
  return data as T;
}

function currentRoute(): Route {
  const detailMatch = window.location.pathname.match(/^\/books\/([^/]+)$/);
  if (detailMatch) {
    return { name: 'details', bookId: decodeURIComponent(detailMatch[1]) };
  }

  const match = window.location.pathname.match(/^\/reader\/([^/]+)$/);
  if (!match) {
    return { name: 'library' };
  }
  return { name: 'reader', bookId: decodeURIComponent(match[1]) };
}

function errorMessage(error: unknown): string {
  if (error instanceof Error) {
    return error.message;
  }
  return 'Erro inesperado';
}

function formatDate(value: string): string {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return value;
  }
  return date.toLocaleString('pt-BR', {
    year: 'numeric',
    month: 'short',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  });
}

function formatBytes(value: number): string {
  if (value === 0) {
    return '0 B';
  }

  const units = ['B', 'KB', 'MB', 'GB'];
  const index = Math.min(Math.floor(Math.log(value) / Math.log(1024)), units.length - 1);
  const amount = value / 1024 ** index;
  return `${amount.toFixed(index === 0 ? 0 : 1)} ${units[index]}`;
}

function readingStats(book: Book, progress: ReadingProgress | null) {
  const totalPages = progress?.total_pages ?? book.page_count;
  const currentPage = progress?.current_page ?? 1;
  const fraction = progress?.fraction ?? null;

  if (totalPages && totalPages > 0) {
    const clampedPage = clampPage(currentPage, totalPages);
    const pagesLeft = Math.max(totalPages - clampedPage, 0);
    const percent = Math.round((clampedPage / totalPages) * 100);
    return {
      label: 'Progresso',
      primary: `${clampedPage} / ${totalPages} páginas`,
      remaining: `${pagesLeft} páginas`,
      timeLeft: pagesLeft === 0 ? 'Concluído' : formatDuration(pagesLeft * 2),
      percent,
    };
  }

  if (fraction != null) {
    const percent = Math.round(clampNumber(fraction, 0, 1) * 100);
    return {
      label: 'Progresso',
      primary: `${percent}%`,
      remaining: `${100 - percent}%`,
      timeLeft: 'Desconhecido',
      percent,
    };
  }

  return {
    label: 'Progresso',
    primary: 'Não iniciado',
    remaining: 'Desconhecido',
    timeLeft: 'Desconhecido',
    percent: 0,
  };
}

function formatDuration(minutes: number): string {
  if (minutes < 60) {
    return `${minutes} min`;
  }
  const hours = Math.floor(minutes / 60);
  const remaining = minutes % 60;
  if (remaining === 0) {
    return `${hours}h`;
  }
  return `${hours}h ${remaining}m`;
}

function statusLabel(status: Book['status']): string {
  switch (status) {
    case 'discovered':
      return 'Descoberto';
    case 'changed':
      return 'Alterado';
    case 'missing':
      return 'Ausente';
  }
}

function metadataJobStatusLabel(status: MetadataJob['status']): string {
  switch (status) {
    case 'queued':
      return 'Na fila';
    case 'running':
      return 'Executando';
    case 'succeeded':
      return 'Concluído';
    case 'failed':
      return 'Com erro';
  }
}

function annotationKindLabel(kind: BookAnnotation['kind']): string {
  if (kind === 'note') {
    return 'Nota';
  }
  return 'Marcação';
}

function annotationLocationLabel(annotation: BookAnnotation): string {
  if (annotation.page_number) {
    if (annotation.total_pages) {
      return `Página ${annotation.page_number} de ${annotation.total_pages}`;
    }
    return `Página ${annotation.page_number}`;
  }
  if (annotation.fraction != null) {
    return `Progresso ${Math.round(annotation.fraction * 100)}%`;
  }
  return 'Localização salva';
}

function viewTitle(view: AppView): string {
  switch (view) {
    case 'catalog':
      return 'Catálogo';
    case 'settings':
      return 'Configurações';
    case 'metadata':
      return 'Metadados';
  }
}

function displayTitle(book: Book): string {
  const extension = book.extension;
  if (extension && book.filename.toLowerCase().endsWith(extension.toLowerCase())) {
    return book.filename.slice(0, -extension.length).trim() || book.filename;
  }
  return book.filename;
}

function groupBooksIntoShelves(books: Book[], folderPath: string): Array<{ label: string; books: Book[] }> {
  const shelves = new Map<string, Book[]>();

  for (const book of books) {
    const rawLabel = folderPath ? book.folder || folderPath : book.category || book.folder?.split('/')[0] || 'Sem categoria';
    const label = formatLabel(rawLabel);
    const shelf = shelves.get(label) ?? [];
    shelf.push(book);
    shelves.set(label, shelf);
  }

  return Array.from(shelves, ([label, shelfBooks]) => ({ label, books: shelfBooks }));
}

function formatLabel(value: string): string {
  return value
    .split('/')
    .filter(Boolean)
    .map((part) =>
      part
        .replace(/[_-]+/g, ' ')
        .replace(/\s+/g, ' ')
        .trim()
        .toLocaleLowerCase('pt-BR')
        .replace(/(^|\s)\p{L}/gu, (letter) => letter.toLocaleUpperCase('pt-BR')),
    )
    .join(' / ');
}

function coverClass(book: Book): string {
  const value = `${book.category}/${book.filename}`;
  let hash = 0;
  for (let index = 0; index < value.length; index++) {
    hash = (hash * 31 + value.charCodeAt(index)) >>> 0;
  }
  return `cover-${hash % 8}`;
}

function parentFolder(folderPath: string): string {
  const index = folderPath.lastIndexOf('/');
  if (index < 0) {
    return '';
  }
  return folderPath.slice(0, index);
}

function clampPage(value: number, totalPages: number): number {
  if (!Number.isFinite(value)) {
    return 1;
  }
  return Math.min(Math.max(Math.round(value), 1), totalPages);
}

function applyEBookSettings(view: FoliateViewElement, settings: EBookSettings) {
  const renderer = view.renderer;
  if (!renderer) {
    return;
  }

  renderer.setAttribute('flow', settings.flow);
  renderer.setAttribute('max-inline-size', `${settings.maxWidth}px`);
  renderer.setAttribute('margin', '12px');
  renderer.setAttribute('gap', '4%');
  renderer.setAttribute('max-column-count', '1');
  renderer.setStyles?.(ebookContentCSS(settings));
}

function ebookContentCSS(settings: EBookSettings) {
  const theme = readerThemeColors(settings.theme);
  const fontSize = Math.round(16 * settings.zoom);
  const fontFamily = settings.fontFamily === 'sans'
    ? 'ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif'
    : 'Georgia, "Times New Roman", serif';

  return `
    :root {
      color-scheme: ${settings.theme === 'dark' ? 'dark' : 'light'};
      background: ${theme.surface} !important;
      color: ${theme.fg} !important;
    }

    html {
      min-height: 0 !important;
      height: auto !important;
    }

    body {
      background: ${theme.surface} !important;
      color: ${theme.fg} !important;
      font-family: ${fontFamily} !important;
      font-size: ${fontSize}px !important;
      line-height: ${settings.lineHeight} !important;
      height: auto !important;
      min-height: 0 !important;
      margin: 0 !important;
    }

    p,
    li,
    blockquote {
      line-height: ${settings.lineHeight} !important;
      margin-block-start: 0;
      margin-block-end: 1em;
    }

    h1,
    h2,
    h3,
    h4,
    h5,
    h6 {
      margin-block-start: 1.5em;
      margin-block-end: .75em;
    }

    body > * {
      max-width: 100%;
      min-height: 0 !important;
    }

    a {
      color: ${theme.link} !important;
    }

    img,
    svg,
    video {
      max-width: 100% !important;
      height: auto;
      object-fit: contain;
    }

    table {
      max-width: 100%;
      overflow-x: auto;
    }
  `;
}

function readerThemeColors(theme: ReaderTheme) {
  if (theme === 'dark') {
    return {
      surface: '#111827',
      fg: '#f8fafc',
      link: '#93c5fd',
    };
  }

  if (theme === 'sepia') {
    return {
      surface: '#f8f0df',
      fg: '#2f281f',
      link: '#7c4a13',
    };
  }

  return {
    surface: '#ffffff',
    fg: '#111827',
    link: '#2563eb',
  };
}

function clampNumber(value: number, min: number, max: number): number {
  if (!Number.isFinite(value)) {
    return min;
  }
  return Math.min(Math.max(value, min), max);
}

function useStoredSettings<T>(key: string, fallback: T): [T, (value: T) => void] {
  const [settings, setSettings] = useState<T>(() => {
    try {
      const raw = window.localStorage.getItem(key);
      if (!raw) {
        return fallback;
      }
      return { ...fallback, ...JSON.parse(raw) } as T;
    } catch {
      return fallback;
    }
  });

  function updateSettings(value: T) {
    setSettings(value);
    window.localStorage.setItem(key, JSON.stringify(value));
  }

  return [settings, updateSettings];
}

async function loadPDFJS() {
  if (!pdfjsPromise) {
    pdfjsPromise = import('pdfjs-dist').then((pdfjs) => {
      pdfjs.GlobalWorkerOptions.workerSrc = pdfWorkerURL;
      return pdfjs;
    });
  }
  return pdfjsPromise;
}
