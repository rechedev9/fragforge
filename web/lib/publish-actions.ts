import {
  YOUTUBE_STUDIO_URL,
  type PublishAssistant,
  type PublishRecommendation,
} from './api/publish-assistant.ts';

export type PublishDraft = {
  title: string;
  description: string;
  tags: string[];
};

export function initialPublishDraft(assistant: PublishAssistant): PublishDraft {
  return {
    title: assistant.metadata.title,
    description: assistant.metadata.description,
    tags: [...assistant.metadata.tags],
  };
}

export function recommendedPublishDraft(recommendation: PublishRecommendation): PublishDraft {
  return {
    title: recommendation.title,
    description: recommendation.description,
    tags: [...recommendation.tags],
  };
}

export async function copyPublishText(value: string): Promise<void> {
  await navigator.clipboard.writeText(value);
}

export function publishTagsText(tags: string[]): string {
  return tags.join(', ');
}

export function downloadPublishMP4(url: string, title: string): void {
  const safeTitle = title.trim().replace(/[<>:"/\\|?*\u0000-\u001f]/g, '-') || 'fragforge-reel';
  const anchor = document.createElement('a');
  anchor.href = url;
  anchor.download = `${safeTitle}.mp4`;
  anchor.rel = 'noopener';
  document.body.appendChild(anchor);
  anchor.click();
  anchor.remove();
}

export function openYouTubeStudio(): void {
  window.open(YOUTUBE_STUDIO_URL, '_blank', 'noopener,noreferrer');
}
