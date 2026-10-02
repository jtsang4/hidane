import { fireEvent, screen } from "@testing-library/svelte";
import { tick } from "svelte";

/** Open a bits-ui Select from its trigger and pick an option by its visible name, as a person would. */
export async function choose(trigger: HTMLElement, option: string | RegExp): Promise<void> {
  await fireEvent.keyDown(trigger, { key: "Enter" });
  await tick();
  const item = await screen.findByRole("option", { name: option });
  await fireEvent.pointerUp(item);
  await tick();
}
