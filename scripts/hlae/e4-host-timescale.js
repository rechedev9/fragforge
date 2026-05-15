"use strict";
{
    const id = "zackvideo/e4-host-timescale";

    if (globalThis[id] !== undefined) {
        globalThis[id].unregister();
        delete globalThis[id];
    }

    const outDir = "__ZV_OUT_DIR__";
    const recDir = `${outDir}/e4-rec`;

    const schedule = [
        { tick: 25, key: "record-name", cmd: `mirv_streams record name "${recDir}"` },
        { tick: 26, key: "record-fps", cmd: "mirv_streams record fps 60" },
        { tick: 27, key: "screen-enabled", cmd: "mirv_streams record screen enabled 1" },
        { tick: 28, key: "screen-set-ffmpeg", cmd: "mirv_streams record screen settings afxFfmpegYuv420p" },
        { tick: 29, key: "clean-view", cmd: "spec_show_xray 0; cl_drawhud 0" },

        { tick: 50, key: "seek-seg-001", cmd: "demo_gototick 21958" },
        { tick: 22022, key: "camera-target", cmd: "spec_mode 1; spec_player_by_accountid 188721128" },
        { tick: 22080, key: "hide-demoui", cmd: "demoui" },
        { tick: 22080, key: "timescale-2", cmd: "host_timescale 2" },
        { tick: 22086, key: "record-start-seg-001", cmd: "mirv_streams record start" },
        { tick: 22406, key: "record-end-seg-001", cmd: "mirv_streams record end" },
        { tick: 22410, key: "timescale-1", cmd: "host_timescale 1" },
        { tick: 22500, key: "disconnect", cmd: "disconnect" },
        { tick: 22600, key: "quit", cmd: "quit" }
    ];

    const fired = {};
    let armed = false;
    let lastTick = null;
    const run = (item) => {
        if (fired[item.key]) return;
        fired[item.key] = true;
        mirv.message(`[zackvideo] ${item.key}: ${item.cmd}\n`);
        mirv.exec(item.cmd);
    };

    mirv.events.clientFrameStageNotify.on(id, (e) => {
        if (e.isBefore) return;
        const tick = mirv.getDemoTick();
        if (!armed) {
            if (lastTick !== null && tick < lastTick) {
                armed = true;
                mirv.message(`[zackvideo] demo playback armed at tick ${tick}\n`);
            }
            lastTick = tick;
            return;
        }
        for (const item of schedule) {
            if (!fired[item.key] && tick >= item.tick) run(item);
        }
    });

    globalThis[id] = {
        unregister: () => mirv.events.clientFrameStageNotify.off(id)
    };
}
