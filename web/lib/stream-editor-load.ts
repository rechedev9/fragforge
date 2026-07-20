export type StreamEditorLoad = {
  generation: number;
  jobId: string;
};

export function nextStreamEditorLoad(current: StreamEditorLoad, jobId: string): StreamEditorLoad {
  return { generation: current.generation + 1, jobId };
}

export function isCurrentStreamEditorLoad(candidate: StreamEditorLoad, current: StreamEditorLoad): boolean {
  return candidate.generation === current.generation && candidate.jobId === current.jobId;
}
