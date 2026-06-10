import React from 'react';
import { SkeletonBlock, Skeleton } from '../common/Skeleton';

interface MessageSkeletonProps {
  isUser?: boolean;
}

const MessageSkeleton: React.FC<MessageSkeletonProps> = ({ isUser = false }) => {
  return (
    <div className={`message-skeleton ${isUser ? 'message-skeleton--user' : 'message-skeleton--assistant'}`}>
      {!isUser && (
        <Skeleton width="32px" height="32px" className="message-avatar-skeleton" />
      )}
      <div className="message-content-skeleton">
        <SkeletonBlock lines={3} />
      </div>
    </div>
  );
};

export default MessageSkeleton;
