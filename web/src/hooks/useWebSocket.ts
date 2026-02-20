"use client";
import { useEffect, useRef, useCallback, useState } from "react";

interface WSEvent {
  type: string;
  entity_id: string;
  timestamp: string;
}

export function useWebSocket(onEvent?: (event: WSEvent) => void) {
  const ws = useRef<WebSocket | null>(null);
  const [connected, setConnected] = useState(false);
  const onEventRef = useRef(onEvent);
  onEventRef.current = onEvent;

  const connect = useCallback(() => {
    const url =
      process.env.NEXT_PUBLIC_WS_URL || "ws://localhost:8080/api/ws";
    const socket = new WebSocket(url);

    socket.onopen = () => setConnected(true);
    socket.onclose = () => {
      setConnected(false);
      setTimeout(connect, 3000);
    };
    socket.onmessage = (e) => {
      try {
        const event: WSEvent = JSON.parse(e.data);
        onEventRef.current?.(event);
      } catch {
        /* ignore malformed messages */
      }
    };

    ws.current = socket;
  }, []);

  useEffect(() => {
    connect();
    return () => ws.current?.close();
  }, [connect]);

  return { connected };
}
