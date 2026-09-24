/** Mailbox addresses of the agent loops. */
export const PRIMARY = "primary";

const MANAGER_PREFIX = "manager:";

export function managerAddress(workItemId: string): string {
  return `${MANAGER_PREFIX}${workItemId}`;
}

export function isManagerAddress(address: string): boolean {
  return address.startsWith(MANAGER_PREFIX);
}

export function workItemIdOf(address: string): string {
  return address.slice(MANAGER_PREFIX.length);
}

export { MANAGER_PREFIX };
