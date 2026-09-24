export type DiffLine = { kind: string; text: string };

export type Step = {
  kind: "thinking" | "tool";
  time: string;
  text?: string;
  name?: string;
  args?: string;
  summary?: string;
  result?: string;
  is_error?: boolean;
  pending?: boolean;
  duration_ms?: number;
  diff?: DiffLine[];
};

export type ToolCount = { name: string; count: number };

export type ActivityBlock = {
  type: "activity";
  start_time: string;
  end_time?: string;
  settled: boolean;
  thinking_lines: number;
  steps?: Step[];
  tools?: ToolCount[];
  tool_count: number;
  in_tokens?: number;
  out_tokens?: number;
};

export type MessageBlock = {
  type: "message";
  role: "user" | "assistant";
  time: string;
  html: string;
};

export type ErrorBlock = {
  type: "error";
  time: string;
  text: string;
};

export type Block = MessageBlock | ActivityBlock | ErrorBlock;

export type SessionSummary = {
  id: string;
  title: string;
  updated_at: string;
};

export type SessionList = {
  workspace: string;
  sessions: SessionSummary[];
};

export type SessionData = {
  session_id: string;
  title: string;
  workspace: string;
  updated_at: string;
  blocks: Block[];
};
