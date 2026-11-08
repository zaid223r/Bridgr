import { useCallback, useEffect, useState, type FormEvent } from "react";
import { api, type Problem, type Todo } from "./api/client";

const pageSize = 5;

type DoneFilter = "all" | "open" | "done";

export function App() {
  const [todos, setTodos] = useState<Todo[]>([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(0);
  const [search, setSearch] = useState("");
  const [doneFilter, setDoneFilter] = useState<DoneFilter>("all");
  const [title, setTitle] = useState("");
  const [priority, setPriority] = useState(2);
  const [problem, setProblem] = useState<Problem | null>(null);

  const load = useCallback(async () => {
    const { data, error, response } = await api.GET("/todos", {
      params: {
        query: {
          limit: pageSize,
          offset: page * pageSize,
          sort: "-priority",
          title: search || undefined,
          done: doneFilter === "all" ? undefined : doneFilter === "done",
        },
      },
    });
    if (error) {
      setProblem(error);
      return;
    }
    const count = Number(response.headers.get("X-Total-Count") ?? data.length);
    setProblem(null);
    setTodos(data);
    setTotal(count);
    // Deleting the last item on the last page leaves us past the end: step back.
    const lastPage = Math.max(0, Math.ceil(count / pageSize) - 1);
    if (page > lastPage) setPage(lastPage);
  }, [page, search, doneFilter]);

  useEffect(() => {
    void load();
  }, [load]);

  async function create(e: FormEvent) {
    e.preventDefault();
    const { error } = await api.POST("/todos", { body: { title, priority } });
    if (error) {
      setProblem(error);
      return;
    }
    setTitle("");
    await load();
  }

  async function toggle(todo: Todo) {
    // PATCH sends only the field that changes.
    const { error } = await api.PATCH("/todos/{id}", {
      params: { path: { id: todo.id } },
      body: { done: !todo.done },
    });
    if (error) setProblem(error);
    await load();
  }

  async function remove(todo: Todo) {
    const { error } = await api.DELETE("/todos/{id}", { params: { path: { id: todo.id } } });
    if (error) setProblem(error);
    await load();
  }

  const pages = Math.max(1, Math.ceil(total / pageSize));

  return (
    <main>
      <h1>Todos</h1>
      <p className="hint">
        A React app on a typed client generated from the Go API's OpenAPI document.{" "}
        <a href="/docs">API reference</a>
      </p>

      <form onSubmit={create} className="row">
        <input value={title} onChange={(e) => setTitle(e.target.value)} placeholder="What needs doing?" aria-label="Title" />
        <select value={priority} onChange={(e) => setPriority(Number(e.target.value))} aria-label="Priority">
          <option value={1}>Low</option>
          <option value={2}>Medium</option>
          <option value={3}>High</option>
        </select>
        <button type="submit">Add</button>
      </form>

      <div className="row">
        <input
          value={search}
          onChange={(e) => {
            setSearch(e.target.value);
            setPage(0);
          }}
          placeholder="Search titles"
          aria-label="Search"
        />
        <select
          value={doneFilter}
          onChange={(e) => {
            setDoneFilter(e.target.value as DoneFilter);
            setPage(0);
          }}
          aria-label="Status"
        >
          <option value="all">All</option>
          <option value="open">Open</option>
          <option value="done">Done</option>
        </select>
      </div>

      {problem && (
        <p role="alert" className="problem">
          {problem.title}
          {problem.detail ? `: ${problem.detail}` : ""}
        </p>
      )}

      <ul>
        {todos.map((todo) => (
          <li key={todo.id} className={todo.done ? "done" : ""}>
            <label>
              <input type="checkbox" checked={todo.done} onChange={() => void toggle(todo)} />
              {todo.title}
            </label>
            <span className={`priority p${todo.priority}`}>{["", "low", "medium", "high"][todo.priority] ?? todo.priority}</span>
            <button className="link" onClick={() => void remove(todo)} aria-label={`Delete ${todo.title}`}>
              Delete
            </button>
          </li>
        ))}
        {todos.length === 0 && <li className="empty">Nothing here.</li>}
      </ul>

      <nav className="row pager">
        <button disabled={page === 0} onClick={() => setPage(page - 1)}>
          Previous
        </button>
        <span>
          Page {page + 1} of {pages} · {total} total
        </span>
        <button disabled={page + 1 >= pages} onClick={() => setPage(page + 1)}>
          Next
        </button>
      </nav>
    </main>
  );
}
