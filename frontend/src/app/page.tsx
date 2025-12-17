"use client";

import { useEffect, useState } from "react";

const API_BASE = "http://localhost:8080";

type LineStatus = {
  line_id: string;
  status: string;
  header?: string | null;
  body?: string | null;
  effect?: string | null;
  updated_at: string;
};

type Subscription = {
  id: number;
  line_id: string;
  via_dm: boolean;
  via_guild: boolean;
  created_at: string;
};

export default function Home() {
  const [lines, setLines] = useState<LineStatus[]>([]);
  const [subs, setSubs] = useState<Subscription[]>([]);
  const [selectedLines, setSelectedLines] = useState<Set<string>>(new Set());
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [message, setMessage] = useState<string | null>(null);

  useEffect(() => {
    async function load() {
      try {
        setLoading(true);
        setError(null);

        const [linesRes, subsRes] = await Promise.all([
          fetch(`${API_BASE}/api/lines`),
          fetch(`${API_BASE}/api/subscriptions`),
        ]);

        if (!linesRes.ok) {
          throw new Error("Failed to fetch lines");
        }
        if (!subsRes.ok) {
          throw new Error("Failed to fetch subscriptions");
        }

        const linesData: LineStatus[] = await linesRes.json();
        const subsData: Subscription[] = await subsRes.json();

        setLines(linesData);
        setSubs(subsData);

        // initialize selectedLines from subs
        const initial = new Set<string>();
        subsData.forEach((s) => initial.add(s.line_id));
        setSelectedLines(initial);
      } catch (err: any) {
        setError(err.message ?? "Unknown error");
      } finally {
        setLoading(false);
      }
    }

    load();
  }, []);

  const toggleLine = (lineID: string) => {
    setSelectedLines((prev) => {
      const next = new Set(prev);
      if (next.has(lineID)) {
        next.delete(lineID);
      } else {
        next.add(lineID);
      }
      return next;
    });
  };

  const handleSave = async () => {
    try {
      setSaving(true);
      setError(null);
      setMessage(null);

      const body = {
        lines: Array.from(selectedLines),
        via_dm: true,
        via_guild: false,
      };

      const res = await fetch(`${API_BASE}/api/subscriptions`, {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
        },
        body: JSON.stringify(body),
      });

      if (!res.ok) {
        throw new Error(`Save failed with status ${res.status}`);
      }

      setMessage("Subscriptions saved!");
    } catch (err: any) {
      setError(err.message ?? "Save failed");
    } finally {
      setSaving(false);
    }
  };

  return (
    <main className="min-h-screen flex flex-col items-center justify-start p-8">
      <h1 className="text-3xl font-bold mb-4">nyctcord dashboard</h1>
      <p className="mb-6 text-gray-600">
        Select the subway lines you want to subscribe to (for now, this uses a
        fake user with ID 1).
      </p>

      {loading && <p>Loading…</p>}
      {error && (
        <p className="text-red-600 mb-4">
          Error: {error}
        </p>
      )}
      {message && (
        <p className="text-green-600 mb-4">
          {message}
        </p>
      )}

      {!loading && !error && (
        <>
          {lines.length === 0 ? (
            <p className="mb-4">
              No line status data yet. Insert some rows into the{" "}
              <code>line_status</code> table or connect the MTA poller later.
            </p>
          ) : (
            <div className="w-full max-w-xl border rounded p-4">
              <h2 className="text-xl font-semibold mb-2">Lines</h2>
              <ul className="space-y-2">
                {lines.map((line) => (
                  <li
                    key={line.line_id}
                    className="flex items-center justify-between"
                  >
                    <label className="flex items-center space-x-2">
                      <input
                        type="checkbox"
                        checked={selectedLines.has(line.line_id)}
                        onChange={() => toggleLine(line.line_id)}
                      />
                      <span className="font-mono">{line.line_id}</span>
                    </label>
                    <span className="text-sm text-gray-600">
                      {line.status}
                    </span>
                  </li>
                ))}
              </ul>
            </div>
          )}

          <button
            onClick={handleSave}
            disabled={saving}
            className="mt-4 px-4 py-2 rounded bg-blue-600 text-white disabled:opacity-50"
          >
            {saving ? "Saving…" : "Save subscriptions"}
          </button>
        </>
      )}
    </main>
  );
}
