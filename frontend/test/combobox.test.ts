import { fireEvent, render, screen } from "@testing-library/svelte";
import { tick } from "svelte";
import { describe, expect, it, vi } from "vitest";
import Combobox from "../src/components/ui/Combobox.svelte";

const suggestions = [{ value: "gpt-6.1-sol", label: "GPT-6.1-Sol" }, { value: "gpt-6-astra" }];

function renderBox(value = "") {
  const onchange = vi.fn();
  render(Combobox, { props: { value, suggestions, onchange, label: "Model", emptyLabel: "Default model" } });
  return { onchange, input: screen.getByRole("combobox", { name: "Model" }) as HTMLInputElement };
}

async function type(input: HTMLInputElement, text: string): Promise<void> {
  // A keystroke opens the list, as typing does; the input event carries the text.
  await fireEvent.keyDown(input, { key: text.slice(-1) });
  await fireEvent.input(input, { target: { value: text } });
  await tick();
}

describe("Combobox", () => {
  it("shows a suggestion's id once, with its display name beside it", async () => {
    const { input } = renderBox();
    await fireEvent.keyDown(input, { key: "ArrowDown" });
    await tick();
    const options = await screen.findAllByRole("option");
    expect(options.map((option) => option.textContent?.replace(/\s+/g, " ").trim())).toEqual(["Default model", "gpt-6.1-sol GPT-6.1-Sol", "gpt-6-astra"]);
  });

  it("opens the list on a click in the field, not only on the chevron", async () => {
    const { input } = renderBox("gpt-6-astra");
    expect(screen.queryByRole("option")).toBeNull();
    await fireEvent.click(input);
    await tick();
    const options = await screen.findAllByRole("option");
    expect(options[0]).toHaveTextContent("gpt-6-astra");
  });

  it("takes what was typed on Enter, even when it is the start of a suggestion", async () => {
    const { input, onchange } = renderBox();
    await type(input, "gpt-6");
    expect(screen.getAllByRole("option")[0]).toHaveTextContent("gpt-6");
    await fireEvent.keyDown(input, { key: "Enter" });
    expect(onchange).toHaveBeenLastCalledWith("gpt-6");
  });

  it("takes a suggestion the person moved to", async () => {
    const { input, onchange } = renderBox();
    await type(input, "astra");
    await fireEvent.keyDown(input, { key: "ArrowDown" });
    await fireEvent.keyDown(input, { key: "Enter" });
    expect(onchange).toHaveBeenLastCalledWith("gpt-6-astra");
    expect(input).toHaveValue("gpt-6-astra");
  });

  it("offers the empty value first and lists the current one at the top", async () => {
    const { input, onchange } = renderBox("my-own-model");
    await fireEvent.keyDown(input, { key: "ArrowDown" });
    await tick();
    const options = await screen.findAllByRole("option");
    expect(options.map((option) => option.textContent?.replace(/\s+/g, " ").trim())).toEqual(["my-own-model", "Default model", "gpt-6.1-sol GPT-6.1-Sol", "gpt-6-astra"]);
    await fireEvent.pointerUp(options[1]!);
    expect(onchange).toHaveBeenLastCalledWith("");
    expect(input).toHaveValue("");
  });

  it("commits trimmed text on blur and on Enter with the list closed", async () => {
    const { input, onchange } = renderBox();
    await fireEvent.input(input, { target: { value: "  custom-1 " } });
    await fireEvent.blur(input);
    expect(onchange).toHaveBeenLastCalledWith("custom-1");
    await fireEvent.input(input, { target: { value: "custom-2" } });
    await fireEvent.keyDown(input, { key: "Enter" });
    expect(onchange).toHaveBeenLastCalledWith("custom-2");
  });

  it("lists a repeated suggestion once", async () => {
    const onchange = vi.fn();
    render(Combobox, { props: { value: "", suggestions: [{ value: "m" }, { value: "m" }], onchange, label: "Model", emptyLabel: "Default model" } });
    await fireEvent.keyDown(screen.getByRole("combobox", { name: "Model" }), { key: "ArrowDown" });
    await tick();
    expect(await screen.findAllByRole("option")).toHaveLength(2);
  });

  it("takes the empty value on Enter once the field is cleared, as blur does", async () => {
    const { input, onchange } = renderBox("gpt-6-astra");
    // A click in the field opens the list, so Enter goes to its highlighted option.
    await fireEvent.click(input);
    await fireEvent.keyDown(input, { key: "Backspace" });
    await fireEvent.input(input, { target: { value: "" } });
    await settle();
    expect((await screen.findAllByRole("option"))[0]).toHaveTextContent("Default model");
    await fireEvent.keyDown(input, { key: "Enter" });
    expect(onchange).toHaveBeenLastCalledWith("");
  });

  it("takes the empty option on Enter when the person moves to it", async () => {
    const { input, onchange } = renderBox("gpt-6-astra");
    await fireEvent.keyDown(input, { key: "ArrowDown" });
    await settle();
    await fireEvent.keyDown(input, { key: "ArrowDown" });
    await fireEvent.keyDown(input, { key: "Enter" });
    expect(onchange).toHaveBeenLastCalledWith("");
  });
});

/** bits-ui moves its highlight a tick after the list changes. */
async function settle(): Promise<void> {
  await tick();
  await tick();
}
