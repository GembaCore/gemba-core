import {
  NewProjectDraftPreview,
  type NewProjectDraftPreviewProps,
} from '@/components/interactions/NewProjectDraftPreview';

export type PlanPreviewPaneProps = NewProjectDraftPreviewProps;

export function PlanPreviewPane(props: PlanPreviewPaneProps): JSX.Element {
  return <NewProjectDraftPreview {...props} />;
}
