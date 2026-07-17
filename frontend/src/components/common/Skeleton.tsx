interface SkeletonProps {
  count?: number
}

// 骨架行数量固定且无交互，索引键不会参与业务状态映射。
const Skeleton = ({ count = 5 }: SkeletonProps) => (
  <div className="skeleton-list" aria-hidden="true">
    {Array.from({ length: count }).map((_, index) => (
      <div key={index} className="skeleton-item">
        <div className="skeleton-line skeleton-title" />
        <div className="skeleton-line skeleton-text" />
        <div className="skeleton-line skeleton-text short" />
      </div>
    ))}
  </div>
)

export default Skeleton
