import type { Match, Play, Song, Video, FeedItem, RenderMode, DemoPlayer, Preset, EditConfig, CaptureReadiness, RosterMatch, SeriesDemo } from './types';
import type { SeriesSummary } from './jobs-index';
import type { PublishAssistant } from './publish-assistant';

export interface ApiClient {
  /** Whether gameplay capture (HLAE + CS2) is configured on the local machine. */
  getCaptureReadiness(): Promise<CaptureReadiness>;
  listMatches(): Promise<Match[]>;
  /** One summary per uploaded series, so Partidas can surface series after a restart. */
  listSeriesSummaries(): Promise<SeriesSummary[]>;
  getMatch(id: string): Promise<Match | null>;
  /** @deprecated Superseded by scanDemo + parseDemo (roster scan → target pick). */
  uploadDemo(input: { fileName: string }): Promise<Match>;
  /**
   * Roster scan: returns the demo's players (K/D/A) so the user can pick a target.
   * Pass `opts.seriesId` to tag the upload as one demo of a bulk bo3/bo5 series.
   */
  scanDemo(file: File, opts?: { seriesId?: string }): Promise<{ jobId: string; players: DemoPlayer[]; match?: RosterMatch }>;
  /** Lists the demos uploaded under one bulk series (bo3/bo5), in upload order. */
  getSeries(seriesId: string): Promise<SeriesDemo[]>;
  /** Parse the scanned demo for the chosen player and return its Match. */
  parseDemo(input: { jobId: string; steamId: string }): Promise<Match>;
  findClips(matchId: string): Promise<Play[]>;
  listSongs(): Promise<Song[]>;
  /** The user-selectable reel presets (preset name == render variant). */
  listPresets(): Promise<Preset[]>;
  /** playIds is 2+ ids for a concatenated reel; pass them in plan order, not click order. */
  createVideo(input: { matchId: string; playIds: string[]; mode: RenderMode; songId?: string; musicVolume?: number; variant?: string; editConfig?: EditConfig }): Promise<Video>;
  listVideos(): Promise<Video[]>;
  getVideo(id: string): Promise<Video | null>;
  /** Build editable metadata and a Madrid schedule for manual publishing. */
  getPublishAssistant(id: string): Promise<PublishAssistant>;
  /** Re-drive a failed reel from where it failed (re-record or re-render). */
  retryVideo(id: string): Promise<Video>;
  /** Remove a reel from the library, deleting its rendered artifacts where possible. */
  deleteVideo(id: string): Promise<void>;
  /**
   * Delete a demo job (match) and its server-side artifacts, pruning any local
   * reels forged from it. A 404 counts as success (already gone); a 409 (job
   * still processing) or a 503 (offline) throws with the body's error/code.
   */
  deleteMatch(jobId: string): Promise<void>;
  /** Delete every demo of a bulk series (bo3/bo5); a 404 on one part is fine. */
  deleteSeries(seriesId: string): Promise<void>;
  listFeed(): Promise<FeedItem[]>;
}
