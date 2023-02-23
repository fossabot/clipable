"use client";
import "shaka-player-react/dist/controls.css";

import { useCallback, useEffect, useRef, useState } from "react";
import ShakaPlayer from "shaka-player-react";

export default function Player({ id }: { id: string }) {
  const videoRef = useRef<any>(null);

  const [volume, setVolume] = useState(parseInt(localStorage.getItem("volume") || "80", 10));
  const [, updateState] = useState<any>();
  const forceUpdate = useCallback(() => updateState({}), []);

  useEffect(() => {
    const { player, videoElement } = videoRef.current;
    console.log("player", player);
    console.log("videoElement", videoElement);

    async function loadVideo() {
      while (!player) {
        // call forceUpdate to rerender the component in a loop until player is defined
        forceUpdate();
        await new Promise((resolve) => setTimeout(resolve, 100));
      }
      await player.load(`http://localhost:3000/api/clips/${id}/dash.mpd`);

      videoElement.play();

      videoElement.addEventListener("volumechange", () => {
        console.log("volumechange", videoElement.volume);
        localStorage.setItem("volume", (videoElement.volume * 100).toString());
        setVolume(videoElement.volume * 100);
      });
    }

    loadVideo();
  }, [id, forceUpdate]);

  return <ShakaPlayer ref={videoRef} volume={volume} />;
}
