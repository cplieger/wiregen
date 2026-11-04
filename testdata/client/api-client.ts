// A recording transport with the shape the generated client imports: it
// records every EndpointSpec and answers from the responder a test sets.

export type HttpMethod = "GET" | "POST" | "PUT" | "PATCH" | "DELETE";

export interface EndpointSpec {
  readonly method: HttpMethod;
  readonly path: string;
  readonly raw?: true;
  readonly body?: unknown;
  readonly headers?: Readonly<Record<string, string>>;
  readonly codes: readonly string[];
}

export interface ApiOk<T> {
  readonly ok: true;
  readonly data: T;
}

export interface Failure<E extends string> {
  readonly ok: false;
  readonly status: number;
  readonly code: E | "http" | "decode";
}

export type ApiResult<T, E extends string = never> = ApiOk<T> | Failure<E>;

export interface Answer {
  readonly status: number;
  readonly body?: unknown;
}

export const recorded: EndpointSpec[] = [];

let responder: (spec: EndpointSpec) => Answer = () => ({ status: 204 });

export function respondWith(fn: (spec: EndpointSpec) => Answer): void {
  responder = fn;
}

export function clientRequest<T, E extends string>(
  spec: EndpointSpec,
  decode: (v: unknown) => T,
  _opts?: { readonly signal?: AbortSignal; readonly headers?: Readonly<Record<string, string>> },
): Promise<ApiResult<T, E>> {
  recorded.push(spec);
  const answer = responder(spec);
  if (answer.status >= 200 && answer.status < 300) {
    const body = spec.raw ? new Blob([String(answer.body)], { type: "image/jpeg" }) : answer.body;
    try {
      return Promise.resolve({ ok: true, data: decode(body) });
    } catch {
      return Promise.resolve({ ok: false, status: answer.status, code: "decode" });
    }
  }
  const code = (answer.body as { code?: unknown } | undefined)?.code;
  if (typeof code === "string" && spec.codes.includes(code)) {
    return Promise.resolve({ ok: false, status: answer.status, code: code as E });
  }
  return Promise.resolve({ ok: false, status: answer.status, code: "http" });
}
