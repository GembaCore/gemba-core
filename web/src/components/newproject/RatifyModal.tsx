import {
  NewProjectRatifyModal,
  type NewProjectRatifyModalProps,
} from '@/components/interactions/NewProjectRatifyModal';

export type RatifyModalProps = NewProjectRatifyModalProps;

export function RatifyModal(props: RatifyModalProps): JSX.Element | null {
  return <NewProjectRatifyModal {...props} />;
}
