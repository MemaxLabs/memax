import type { MemaxConfig } from "./types.js";
import { ApiTransport } from "./transport.js";
import { MemoriesResource } from "./resources/memories.js";
import { ConfigsResource } from "./resources/configs.js";
import { AgentSessionsResource } from "./resources/agent-sessions.js";
import { AuthResource } from "./resources/auth.js";
import { HubsResource } from "./resources/hubs.js";
import { TopicsResource } from "./resources/topics.js";
import { SettingsResource } from "./resources/settings.js";
import { UploadsResource } from "./resources/uploads.js";
import { DreamsResource } from "./resources/dreams.js";
import { NotificationsResource } from "./resources/notifications.js";
import { AgentsResource } from "./resources/agents.js";
import { InvitesResource } from "./resources/invites.js";
import { AccountResource } from "./resources/account.js";
import { EventsResource } from "./resources/events.js";
import { BarResource } from "./resources/bar.js";

export class Memax {
  readonly memories: MemoriesResource;
  readonly configs: ConfigsResource;
  readonly agentSessions: AgentSessionsResource;
  readonly uploads: UploadsResource;
  readonly auth: AuthResource;
  readonly account: AccountResource;
  readonly hubs: HubsResource;
  readonly invites: InvitesResource;
  readonly topics: TopicsResource;
  readonly settings: SettingsResource;
  readonly dreams: DreamsResource;
  readonly notifications: NotificationsResource;
  readonly agents: AgentsResource;
  readonly events: EventsResource;
  readonly bar: BarResource;

  constructor(config: MemaxConfig) {
    const transport = new ApiTransport(config);
    const req = transport.request.bind(transport);
    const stream = transport.stream.bind(transport);
    const download = transport.download.bind(transport);
    // Read the resolved URL from the transport so the DEFAULT_API_URL
    // fallback lives in exactly one place. Duplicating the hardcoded
    // literal here would let the transport-level drift guard pass
    // while `auth.githubURL(...)` silently used a stale host.
    const apiUrl = transport.apiUrl;

    this.memories = new MemoriesResource(req, stream, download);
    this.configs = new ConfigsResource(req);
    this.agentSessions = new AgentSessionsResource(req, download);
    this.uploads = new UploadsResource(req);
    this.auth = new AuthResource(req, apiUrl);
    this.account = new AccountResource(req);
    this.hubs = new HubsResource(req);
    this.invites = new InvitesResource(req);
    this.topics = new TopicsResource(req);
    this.settings = new SettingsResource(req);
    this.dreams = new DreamsResource(req);
    this.notifications = new NotificationsResource(req);
    this.agents = new AgentsResource(req);
    this.events = new EventsResource(stream);
    this.bar = new BarResource(req);
  }

  async push(...args: Parameters<MemoriesResource["push"]>) {
    return this.memories.push(...args);
  }

  async recall(...args: Parameters<MemoriesResource["recall"]>) {
    return this.memories.recall(...args);
  }

  async ask(...args: Parameters<MemoriesResource["ask"]>) {
    return this.memories.ask(...args);
  }
}
