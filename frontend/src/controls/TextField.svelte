<script>
  import InputLabel from "./InputLabel.svelte";
  import Button from "./Button.svelte";
  import {v4 as uuid} from "uuid";

  export let label = undefined;
  export let hint = "";
  export let placeholder = "";
  export let width = "400px";
  export let style = "";
  export let value = "";
  export let labelColor = undefined;
  export let removeable = false;
  function setUUID() {
    value = uuid()
  }
</script>

<style>
  .text-field {
    margin-bottom: var(--padding);
  }
  input {
    box-sizing: border-box;
    width: 100%;
    background-color: var(--bg-input-color);
    border: var(--border);
    outline: none;
    color: var(--text-color);
    padding: var(--padding);
    font-size: var(--font-size);
  }
  input::selection {
    background-color: var(--accent-color2);
  }
  input::placeholder {
    color: var(--text-color3);
  }
  .withid {
    display: flex;
    flex-direction: row;
    align-items: center;
    justify-content: center;
    gap: 10px;
  }
</style>

<div class="text-field" style="width:{width};{style}">
  {#if label}
    <InputLabel on:remove {removeable} {label} {hint} color={labelColor} />
  {/if}
  <div class="withid">
    <input on:focus on:input autocapitalize="off" autocorrect="off" autocomplete="off" type="text" placeholder={placeholder} bind:value />
    {#if typeof label == "string" && (label == 'id' || String(label).includes("_id"))}
      <Button text="Generate" on:click={setUUID}/>
    {/if}
    
  </div>
</div>
