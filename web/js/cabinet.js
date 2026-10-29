// The cabinet page: one game, a status badge, keyboard and gamepad. See cabinet.html.
import { api } from "api";
import { sub, CONTROLLER_UPDATED, MESSAGE } from "event";
import { KEY } from "input";
import { retropad } from "./input/retropad.js?v=3";
import { log } from "log";
import { socket, webrtc } from "network";

const SLOT_CHANGE = 151;
const UNKNOWN_GAME = 114;
const COLORS = ["#ef5350", "#42a5f5", "#66bb6a", "#ffee58"];

const params = new URLSearchParams(location.search);
const cabinet = params.get("cabinet") || "cabinet";
const game = params.get("game") || "";
const players = Math.min(Math.max(parseInt(params.get("players")) || 4, 1), 4);
const percent = (s) =>
    isNaN(parseInt(s)) ? null : Math.min(Math.max(parseInt(s), 0), 100);

const video = document.getElementById("stream");
const badge = document.getElementById("badge");
const hint = document.getElementById("hint");
const sound = document.getElementById("sound");
const slider = document.getElementById("volume");
const muteButton = document.getElementById("mute");

let slot = null; // {index, queue}: index -1 is a watcher
let status = "Connecting…";
let playing = false;
let error = ""; // why there's no game, shown in the middle of the screen
let blocked = false; // the browser keeps the sound off until the user interacts with the page

const render = () => {
    const isPlayer = slot?.index >= 0;
    badge.textContent = !slot
        ? status
        : isPlayer
          ? `P${slot.index + 1}`
          : `Watching · #${slot.queue} in line`;
    badge.classList.toggle("player", isPlayer);
    badge.style.background = isPlayer ? COLORS[slot.index] : "";

    // a click both focuses the iframe and allows sound
    let text = "";
    if (error) text = error;
    else if (isPlayer && !document.hasFocus()) text = "Click to play";
    else if (playing && blocked) text = "Click for sound";
    hint.textContent = text;
    hint.hidden = !text;
};

const fail = (short, why) => {
    slot = null;
    status = short;
    error = why;
    render();
};

// sound: the volume the user last chose on this site, or the one in the URL (0 is muted)

const VOLUME_KEY = "cabinet-volume";
const saved = () => {
    try {
        return localStorage.getItem(VOLUME_KEY);
    } catch {
        return null; // storage blocked
    }
};
const urlVolume = percent(params.get("volume"));
let volume = percent(saved()) ?? urlVolume ?? 100;
let unmutedVolume = volume || urlVolume || 100; // what the speaker brings back

const applySound = () => {
    video.muted = blocked || !volume;
    video.volume = volume / 100;
    slider.value = volume;
    sound.dataset.level = video.muted ? 0 : volume < 50 ? 1 : 2;
    muteButton.ariaLabel = video.muted ? "Unmute" : "Mute";
    render();
};
const setVolume = (v) => {
    volume = v;
    if (v) unmutedVolume = v;
    try {
        localStorage.setItem(VOLUME_KEY, v);
    } catch {}
    applySound();
};

slider.addEventListener("input", () => setVolume(+slider.value));
slider.addEventListener("change", () => slider.blur()); // leave the keys to the game
muteButton.addEventListener("click", () => {
    if (blocked) {
        blocked = false;
        if (!volume) setVolume(unmutedVolume);
    } else setVolume(volume ? 0 : unmutedVolume);
    applySound();
    muteButton.blur();
});

// iPhone and iPad ignore the volume, only muting works there
video.volume = 0.5;
if (video.volume !== 0.5) slider.parentElement.remove();
applySound();

// video

const play = () => {
    blocked = false;
    applySound();
    video
        .play()
        .catch(() => {
            // no sound before the user interacts with the page
            blocked = true;
            applySound();
            return video.play();
        })
        .then(
            () => (playing = true),
            (e) => log.error("[cabinet] play", e),
        )
        .finally(render);
};

let serverAspect = 0;
// ponytail: av.rot (vertical arcade games) is ignored, add when such a core is on a cabinet
const setVideo = (av) => {
    serverAspect = av.a || 0;
    if (serverAspect) video.style.setProperty("--a", serverAspect);
    video.style.transform = av.flip ? "scaleY(-1)" : "";
};
video.addEventListener("resize", () => {
    if (!serverAspect && video.videoHeight)
        video.style.setProperty("--a", video.videoWidth / video.videoHeight);
});

const unmute = () => {
    if (playing && blocked) {
        blocked = false;
        applySound();
    }
};
// the speaker handles its own clicks
addEventListener(
    "pointerdown",
    (e) => muteButton.contains(e.target) || unmute(),
);
addEventListener("focus", render);
addEventListener("blur", render);

// input: game keys only, by position (e.code), so they sit in the same place on every keyboard layout

// Show the player's own letters in the help. Only Chromium can tell the layout (Keyboard Map API), and in an iframe
// only with allow="keyboard-map"; elsewhere the help keeps the QWERTY letters plus the AZERTY ones.
navigator.keyboard?.getLayoutMap?.().then(
    (layout) => {
        const keys = [...document.querySelectorAll("#help [data-codes]")];
        const labels = keys.map((el) =>
            el.dataset.codes
                .split(" ")
                .map((code) => layout.get(code)?.toUpperCase()),
        );
        if (!labels.flat().every(Boolean)) return;
        keys.forEach((el, i) => (el.textContent = labels[i].join(" ")));
        document
            .querySelectorAll("#help .other-layout")
            .forEach((el) => el.remove());
    },
    () => {},
);

const keyMap = {
    ArrowUp: KEY.UP,
    ArrowDown: KEY.DOWN,
    ArrowLeft: KEY.LEFT,
    ArrowRight: KEY.RIGHT,
    KeyZ: KEY.A,
    KeyX: KEY.B,
    KeyC: KEY.X,
    KeyV: KEY.Y,
    KeyA: KEY.L,
    KeyS: KEY.R,
    Semicolon: KEY.L2,
    Quote: KEY.R2,
    Period: KEY.L3,
    Slash: KEY.R3,
    Enter: KEY.START,
    ShiftLeft: KEY.SELECT,
    ShiftRight: KEY.SELECT,
};

// W3C standard gamepad buttons, by position (bottom button = B, as on a SNES pad)
const padMap = [
    KEY.B,
    KEY.A,
    KEY.Y,
    KEY.X,
    KEY.L,
    KEY.R,
    KEY.L2,
    KEY.R2,
    KEY.SELECT,
    KEY.START,
    KEY.L3,
    KEY.R3,
    KEY.UP,
    KEY.DOWN,
    KEY.LEFT,
    KEY.RIGHT,
];

const held = { kb: new Set(), pad: new Set() };
const hold = (from, key, on) => {
    on ? held[from].add(key) : held[from].delete(key);
    retropad.setKeyState(key, held.kb.has(key) || held.pad.has(key));
};

const onKey = (on) => (e) => {
    const key = keyMap[e.code];
    if (on) unmute();
    if (!key) return;
    e.preventDefault();
    if (!e.repeat) hold("kb", key, on);
};
addEventListener("keydown", onKey(true));
addEventListener("keyup", onKey(false));
addEventListener("blur", () => held.kb.forEach((k) => hold("kb", k, false)));

const pollPads = () => {
    const now = new Set();
    for (const pad of navigator.getGamepads?.() || []) {
        if (!pad) continue;
        pad.buttons.forEach(
            (b, i) => b.pressed && padMap[i] && now.add(padMap[i]),
        );
        const [x = 0, y = 0] = pad.axes; // the left stick works as a d-pad too
        if (x < -0.5) now.add(KEY.LEFT);
        if (x > 0.5) now.add(KEY.RIGHT);
        if (y < -0.5) now.add(KEY.UP);
        if (y > 0.5) now.add(KEY.DOWN);
        pad.axes
            .slice(0, 4)
            .forEach((v, i) =>
                retropad.setAxisChanged(i, Math.abs(v) < 0.1 ? 0 : v),
            );
    }
    held.pad.forEach((k) => now.has(k) || hold("pad", k, false));
    now.forEach((k) => held.pad.has(k) || hold("pad", k, true));
    requestAnimationFrame(pollPads);
};

sub(CONTROLLER_UPDATED, (data) => webrtc.send("data", data));

// connection

let started = false;
// everyone opening the same cabinet name plays the same game, so the name must be globally unique
const roomId = `${cabinet}___${game}`;

const onMessage = ({ t, p }) => {
    switch (t) {
        case api.endpoint.INIT:
            webrtc.start({
                iceServers: p.ice,
                media: video,
                onDataChannel: (ch) => {
                    if (ch.label === "data")
                        ch.onmessage = (x) => onMessage(api.fromBytes(x.data));
                    return ch;
                },
                onConnect: () => {
                    if (started) return; // ICE restarts reconnect too
                    started = true;
                    socket.send({
                        t: api.endpoint.GAME_START,
                        p: { game_name: game, room_id: roomId, players },
                    });
                },
                onDisconnect: () => {
                    webrtc.stop();
                    fail("Disconnected", "Disconnected, reload to retry");
                },
                signalling: {
                    init: api.server.initWebrtcStream,
                    sendIceCandidate: api.server.sendIceCandidate,
                    sendSdp: api.server.sendSdp,
                },
            });
            break;
        case api.endpoint.WEBRTC_SIGNAL:
            if (Object.hasOwn(p, "ice"))
                webrtc.candidate(p.ice ? api.fromJson(p.ice) : "");
            else if (Object.hasOwn(p, "sdp"))
                webrtc.answer(api.fromJson(p.sdp));
            break;
        case api.endpoint.GAME_START:
            if (p.av) setVideo(p.av);
            slot = p.slot || { index: 0 };
            retropad.toggle(true);
            requestAnimationFrame(pollPads);
            play();
            break;
        case api.endpoint.APP_VIDEO_CHANGE:
            setVideo(p);
            break;
        case SLOT_CHANGE:
            slot = p;
            render();
            break;
        case api.endpoint.GAME_ERROR_NO_FREE_SLOTS:
            fail("Busy", "All game machines are busy, try again later");
            break;
        case UNKNOWN_GAME: // p: the game the name most likely meant, if any
            fail(
                "Unknown game",
                p
                    ? `There's no game "${game}". Did you mean "${p}"?`
                    : `There's no game "${game}" on this server. Use the ROM file name, without its extension.`,
            );
            break;
    }
};
sub(MESSAGE, onMessage);

api.transport = {
    send: socket.send,
    keyboard: () => {},
    mouse: () => {},
};

if (!game) {
    fail("No game", "No game set in the cabinet URL");
} else if (cabinet.includes("___")) {
    fail("Bad cabinet name", "The cabinet name can't contain ___");
} else {
    log.info(`[cabinet] room ${roomId}`);
    socket.init(roomId);
}
