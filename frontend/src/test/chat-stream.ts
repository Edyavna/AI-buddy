export const eventText = (event: string, data: unknown) => `event: ${event}\ndata: ${JSON.stringify(data)}\n\n`;
export function bytes(text: string, split = false) {
  const encoded = new TextEncoder().encode(text);
  return new ReadableStream<Uint8Array>({ start(controller) {
    if (split) for (const byte of encoded) controller.enqueue(new Uint8Array([byte]));
    else controller.enqueue(encoded);
    controller.close();
  } });
}
