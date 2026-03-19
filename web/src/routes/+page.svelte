<script lang="ts">
  import { createClient } from "@connectrpc/connect";
  import { transport } from "$lib/transport";
  import { WebService } from "$lib/connect/holomush/web/v1/web_pb";

  const client = createClient(WebService, transport);

  let sessionId = $state("");
  let characterName = $state("");
  let connected = $state(false);
  let commandText = $state("");
  let events: Array<{ type: string; characterName: string; text: string }> = $state([]);
  let error = $state("");

  async function login() {
    error = "";
    try {
      const resp = await client.login({ username: "guest", password: "" });
      if (resp.success) {
        sessionId = resp.sessionId;
        characterName = resp.characterName;
        connected = true;
        startEventStream();
      } else {
        error = resp.errorMessage || "Login failed";
      }
    } catch (e) {
      error = `Connection error: ${e}`;
    }
  }

  async function startEventStream() {
    try {
      for await (const resp of client.streamEvents({ sessionId })) {
        const ev = resp.event;
        if (!ev) continue;
        events = [
          ...events,
          {
            type: ev.type,
            characterName: ev.characterName,
            text: ev.text,
          },
        ];
      }
    } catch {
      // Stream ended
    }
  }

  async function sendCommand() {
    if (!commandText.trim()) return;
    error = "";
    try {
      await client.sendCommand({ sessionId, text: commandText });
      commandText = "";
    } catch (e) {
      error = `Command error: ${e}`;
    }
  }

  async function disconnect() {
    try {
      await client.disconnect({ sessionId });
    } catch {
      // Best effort
    }
    connected = false;
    sessionId = "";
    characterName = "";
    events = [];
  }
</script>

<main>
  <h1>HoloMUSH Web Client</h1>

  {#if error}
    <p style="color: red">{error}</p>
  {/if}

  {#if !connected}
    <button onclick={login}>Connect as Guest</button>
  {:else}
    <p>Connected as <strong>{characterName}</strong></p>

    <div>
      <h2>Events</h2>
      <ul>
        {#each events as event}
          <li>[{event.type}] {event.characterName}: {event.text}</li>
        {/each}
      </ul>
    </div>

    <form onsubmit={(e) => { e.preventDefault(); sendCommand(); }}>
      <input bind:value={commandText} placeholder="say hello" />
      <button type="submit">Send</button>
    </form>

    <button onclick={disconnect}>Disconnect</button>
  {/if}
</main>
