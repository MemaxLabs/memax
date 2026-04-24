import type { StreamFn } from "../transport.js";

export interface EventsSubscribeOptions {
  hubId?: string;
  onEvent: (event: string, data: unknown) => void;
  onClose?: () => void;
}

export class EventsResource {
  constructor(private readonly stream: StreamFn) {}

  subscribe(options: EventsSubscribeOptions): AbortController {
    return this.stream("GET", "/v1/events/stream", {
      query: {
        hub_id: options.hubId,
      },
      hubId: options.hubId,
      onEvent: options.onEvent,
      onClose: options.onClose,
    });
  }
}
