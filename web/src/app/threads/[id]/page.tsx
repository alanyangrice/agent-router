"use client";

import { useEffect, useState, useRef } from "react";
import { useParams } from "next/navigation";
import { api, Message, Thread, Agent } from "@/lib/api";
import { useWebSocket } from "@/hooks/useWebSocket";
import { DashboardShell } from "@/components/DashboardShell";

const POST_TYPE_STYLES: Record<string, string> = {
  discussion: "bg-blue-600/20 text-blue-400",
  code_review: "bg-purple-600/20 text-purple-400",
  status_update: "bg-green-600/20 text-green-400",
  question: "bg-yellow-600/20 text-yellow-400",
  decision: "bg-orange-600/20 text-orange-400",
};

export default function ThreadPage() {
  const { id } = useParams<{ id: string }>();
  const [thread, setThread] = useState<Thread | null>(null);
  const [messages, setMessages] = useState<Message[]>([]);
  const [agents, setAgents] = useState<Map<string, Agent>>(new Map());
  const [loading, setLoading] = useState(true);
  const [content, setContent] = useState("");
  const [sending, setSending] = useState(false);
  const messagesEndRef = useRef<HTMLDivElement>(null);

  const fetchMessages = async () => {
    try {
      const msgs = await api.listMessages(id);
      setMessages(msgs);
    } catch {
      /* API not available */
    }
  };

  useEffect(() => {
    (async () => {
      try {
        const [threadData, msgs, agentList] = await Promise.all([
          api.listThreads().then((ts) => ts.find((t) => t.id === id) || null),
          api.listMessages(id),
          api.listAgents(),
        ]);
        setThread(threadData);
        setMessages(msgs);
        setAgents(new Map(agentList.map((a) => [a.id, a])));
      } catch {
        /* API not available */
      } finally {
        setLoading(false);
      }
    })();
  }, [id]);

  useEffect(() => {
    messagesEndRef.current?.scrollIntoView({ behavior: "smooth" });
  }, [messages]);

  useWebSocket((event) => {
    if (event.type === "message.created" && event.entity_id) {
      fetchMessages();
    }
  });

  const handleSend = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!content.trim() || sending) return;

    setSending(true);
    try {
      await api.postMessage(id, {
        post_type: "discussion",
        content: content.trim(),
      });
      setContent("");
      await fetchMessages();
    } catch {
      /* handle error */
    } finally {
      setSending(false);
    }
  };

  return (
    <DashboardShell>
      {loading ? (
        <div className="flex h-96 items-center justify-center">
          <div className="h-8 w-8 animate-spin rounded-full border-2 border-gray-600 border-t-blue-500" />
        </div>
      ) : (
        <div className="flex h-[calc(100vh-7rem)] flex-col">
          <div className="mb-4 border-b border-gray-800 pb-4">
            <h1 className="text-xl font-semibold text-white">
              {thread?.name || `Thread ${id.slice(0, 8)}`}
            </h1>
            {thread && (
              <p className="mt-1 text-sm text-gray-400">
                {thread.type}
                {thread.task_id &&
                  ` · Task ${thread.task_id.slice(0, 8)}`}
              </p>
            )}
          </div>

          <div className="flex-1 space-y-4 overflow-y-auto pr-2">
            {messages.length === 0 ? (
              <div className="flex h-40 items-center justify-center text-sm text-gray-500">
                No messages yet. Start the conversation below.
              </div>
            ) : (
              messages.map((msg) => {
                const agent = msg.agent_id
                  ? agents.get(msg.agent_id)
                  : null;
                const senderName = agent?.name || "Human";
                const typeStyle =
                  POST_TYPE_STYLES[msg.post_type] ||
                  "bg-gray-600/20 text-gray-400";

                return (
                  <div
                    key={msg.id}
                    className="rounded-lg border border-gray-800 bg-gray-800/50 p-4"
                  >
                    <div className="mb-2 flex items-center gap-2">
                      <div
                        className={`flex h-7 w-7 items-center justify-center rounded-full text-xs font-semibold ${
                          agent
                            ? "bg-blue-600 text-blue-100"
                            : "bg-gray-600 text-gray-300"
                        }`}
                      >
                        {senderName.charAt(0).toUpperCase()}
                      </div>
                      <span className="text-sm font-medium text-gray-200">
                        {senderName}
                      </span>
                      <span
                        className={`rounded px-1.5 py-0.5 text-[10px] font-medium ${typeStyle}`}
                      >
                        {msg.post_type.replace("_", " ")}
                      </span>
                      <span className="ml-auto text-xs text-gray-500">
                        {new Date(msg.created_at).toLocaleTimeString()}
                      </span>
                    </div>
                    <div className="whitespace-pre-wrap text-sm text-gray-300 leading-relaxed">
                      {msg.content}
                    </div>
                  </div>
                );
              })
            )}
            <div ref={messagesEndRef} />
          </div>

          <form
            onSubmit={handleSend}
            className="mt-4 flex gap-3 border-t border-gray-800 pt-4"
          >
            <input
              type="text"
              value={content}
              onChange={(e) => setContent(e.target.value)}
              placeholder="Type a message..."
              className="flex-1 rounded-lg border border-gray-700 bg-gray-900 px-4 py-2.5 text-sm text-white placeholder-gray-500 outline-none transition-colors focus:border-blue-500 focus:ring-1 focus:ring-blue-500"
            />
            <button
              type="submit"
              disabled={sending || !content.trim()}
              className="rounded-lg bg-blue-600 px-5 py-2.5 text-sm font-medium text-white transition-colors hover:bg-blue-500 disabled:cursor-not-allowed disabled:opacity-50"
            >
              Send
            </button>
          </form>
        </div>
      )}
    </DashboardShell>
  );
}
