import { sql } from "./db.js";
import { genId } from "./ids.js";

/**
 * Channel bindings map runtime threads onto channel-native structures.
 * Feishu mapping: main thread ↔ p2p chat top-level; work item thread ↔ the
 * reply-thread under a bot-posted root message.
 */
export interface ChannelBinding {
  id: string;
  channel: string;
  kind: "main" | "work_item";
  workItemId: string | null;
  chatId: string;
  rootId: string | null;
}

interface BindingRow {
  id: string;
  channel: string;
  kind: string;
  work_item_id: string | null;
  chat_id: string;
  root_id: string | null;
}

function toBinding(row: BindingRow): ChannelBinding {
  return {
    id: row.id,
    channel: row.channel,
    kind: row.kind as ChannelBinding["kind"],
    workItemId: row.work_item_id,
    chatId: row.chat_id,
    rootId: row.root_id,
  };
}

export async function createBinding(input: {
  channel: string;
  kind: "main" | "work_item";
  workItemId?: string | undefined;
  chatId: string;
  rootId?: string | undefined;
}): Promise<ChannelBinding> {
  const db = sql();
  const id = genId("cb", 6);
  await db`
    INSERT INTO channel_bindings (id, channel, kind, work_item_id, chat_id, root_id)
    VALUES (${id}, ${input.channel}, ${input.kind}, ${input.workItemId ?? null},
            ${input.chatId}, ${input.rootId ?? null})`;
  const rows = await db`SELECT * FROM channel_bindings WHERE id = ${id}`;
  return toBinding(rows[0] as unknown as BindingRow);
}

/** Inbound routing: which thread does a channel message belong to? */
export async function findByChannelRef(
  channel: string,
  chatId: string,
  rootId: string | null,
): Promise<ChannelBinding | undefined> {
  const db = sql();
  const rows = rootId
    ? await db`
        SELECT * FROM channel_bindings
        WHERE channel = ${channel} AND chat_id = ${chatId} AND root_id = ${rootId}`
    : await db`
        SELECT * FROM channel_bindings
        WHERE channel = ${channel} AND chat_id = ${chatId} AND root_id IS NULL`;
  return rows.length > 0 ? toBinding(rows[0] as unknown as BindingRow) : undefined;
}

/** Outbound routing: where do a work item's replies go on this channel? */
export async function findByWorkItem(
  channel: string,
  workItemId: string,
): Promise<ChannelBinding | undefined> {
  const db = sql();
  const rows = await db`
    SELECT * FROM channel_bindings
    WHERE channel = ${channel} AND work_item_id = ${workItemId}`;
  return rows.length > 0 ? toBinding(rows[0] as unknown as BindingRow) : undefined;
}
