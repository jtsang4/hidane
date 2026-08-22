import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import Markdown from "react-markdown";
import { api } from "../lib/api.js";
import { today } from "../lib/utils.js";
import { Input } from "../components/ui/primitives.js";

export function LogPage() {
  const [day, setDay] = useState(today());
  const { data } = useQuery({
    queryKey: ["worklog", day],
    queryFn: () => api.worklog(day),
    enabled: /^\d{4}-\d{2}-\d{2}$/.test(day),
  });

  return (
    <div className="space-y-3 p-4">
      <div className="flex items-center justify-between gap-3">
        <h1 className="text-lg font-semibold">工作日志</h1>
        <Input
          type="date"
          className="w-44"
          value={day}
          onChange={(e) => setDay(e.target.value)}
        />
      </div>
      <article className="prose-sm max-w-none space-y-2 [&_h1]:text-xl [&_h1]:font-bold [&_h2]:mt-4 [&_h2]:text-base [&_h2]:font-semibold [&_li]:ml-4 [&_li]:list-disc [&_code]:rounded [&_code]:bg-surface-2 [&_code]:px-1 [&_code]:text-xs">
        <Markdown>{data?.markdown ?? ""}</Markdown>
      </article>
    </div>
  );
}
