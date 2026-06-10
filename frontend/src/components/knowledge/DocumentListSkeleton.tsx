import React from 'react';
import { Skeleton } from '../common/Skeleton';

interface DocumentListSkeletonProps {
  count?: number;
}

const DocumentListSkeleton: React.FC<DocumentListSkeletonProps> = ({ count = 5 }) => {
  return (
    <div className="document-list-skeleton">
      {Array.from({ length: count }).map((_, i) => (
        <div key={i} className="document-item-skeleton">
          <Skeleton width="40px" height="40px" className="doc-icon-skeleton" />
          <div className="doc-info-skeleton">
            <Skeleton width="60%" height="16px" />
            <Skeleton width="40%" height="12px" />
          </div>
        </div>
      ))}
    </div>
  );
};

export default DocumentListSkeleton;
