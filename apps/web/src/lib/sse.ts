// Minimal Server-Sent Events parser over a fetch ReadableStream.
//
// The Go backend streams named events (`start`, `delta`, `done`, `error`) with
// JSON payloads using the standard SSE wire format: `event: <name>`, one or more
// `data: <line>` fields, and a blank line terminating each event. Heartbeats are
// comment lines (`:`) and are ignored.

export interface SSEEvent {
  event: string;
  data: string;
}

export async function* parseSSE(body: ReadableStream<Uint8Array>): AsyncGenerator<SSEEvent> {
  const reader = body.getReader();
  const decoder = new TextDecoder();
  let buffer = "";

  try {
    for (;;) {
      const { done, value } = await reader.read();
      if (done) break;
      buffer += decoder.decode(value, { stream: true });
      buffer = buffer.replace(/\r\n/g, "\n");

      let sep: number;
      while ((sep = buffer.indexOf("\n\n")) !== -1) {
        const block = buffer.slice(0, sep);
        buffer = buffer.slice(sep + 2);
        const evt = parseBlock(block);
        if (evt) yield evt;
      }
    }

    if (buffer.trim()) {
      const evt = parseBlock(buffer);
      if (evt) yield evt;
    }
  } finally {
    reader.releaseLock();
  }
}

function parseBlock(block: string): SSEEvent | null {
  let event = "message";
  const dataLines: string[] = [];

  for (const line of block.split("\n")) {
    if (line.startsWith("event:")) {
      event = stripField(line.slice("event:".length));
    } else if (line.startsWith("data:")) {
      dataLines.push(stripField(line.slice("data:".length)));
    }
    // Comment lines (":") and unknown fields are ignored.
  }

  if (dataLines.length === 0) return null;
  return { event, data: dataLines.join("\n") };
}

// SSE strips a single leading space after the colon, if present.
function stripField(value: string): string {
  return value.startsWith(" ") ? value.slice(1) : value;
}
