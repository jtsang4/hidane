import { beforeAll, beforeEach, afterAll } from "vitest";
import { migrate, sql, closeDb } from "../src/kernel/db.js";

beforeAll(async () => {
  await migrate();
});

beforeEach(async () => {
  const db = sql();
  await db`TRUNCATE events, cursors, work_items, threads, channel_bindings, schedules RESTART IDENTITY CASCADE`;
  await db`INSERT INTO threads (id, kind) VALUES ('main', 'main') ON CONFLICT DO NOTHING`;
});

afterAll(async () => {
  await closeDb();
});
