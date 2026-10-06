// Delivery without the daemon, for `memax compile` when none runs: the
// same passes, in this process, holding the daemon's lock meanwhile so a
// daemon starting at the same moment can't write the same files.
import type { DaemonApi } from "./api.js";
import { AlreadyRunningError, ControlServer } from "./control.js";
import { Daemon } from "./daemon.js";
import type { Logger } from "./log.js";
import type { DaemonPaths } from "./paths.js";
import type { DaemonSnapshot } from "./snapshot.js";

export class OneShotDelivery {
  private constructor(
    private readonly daemon: Daemon,
    private readonly lock: ControlServer,
  ) {}

  /** Null when a daemon holds the lock (then it delivers). */
  static async open(o: {
    paths: DaemonPaths;
    api: DaemonApi;
    deviceId: string;
    log: Logger;
    version: string;
    apiUrl: string;
  }): Promise<OneShotDelivery | null> {
    const daemon = new Daemon(o);
    try {
      const lock = await ControlServer.listen(o.paths, async (req) =>
        req.cmd === "status" ? { ...daemon.snapshot(), oneshot: true } : {},
      );
      return new OneShotDelivery(daemon, lock);
    } catch (err) {
      if (err instanceof AlreadyRunningError) return null;
      throw err;
    }
  }

  /** One pass: poll every linked space once and deliver. */
  sync(): Promise<DaemonSnapshot> {
    return this.daemon.syncOnce();
  }

  async close(): Promise<void> {
    await this.daemon.stop();
    await this.lock.close();
  }
}
