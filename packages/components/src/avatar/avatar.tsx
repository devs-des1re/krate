import './avatar.css';
import { createSignal, createEffect } from '@krate/runtime';

export interface AvatarProps {
  children?: any;
  src?: string;
  alt?: string;
  fallback?: string;
  size?: 'sm' | 'md' | 'lg';
}

export function Avatar(props: AvatarProps) {
  var src = props.src || "";
  var alt = props.alt || "";
  var fallback = props.fallback || "?";
  var size = props.size || "md";
  var [imageError, setImageError] = createSignal(false);

  createEffect(function () {
    if (src === "") {
      setImageError(true);
    }
  });

  function handleError() {
    setImageError(true);
  }

  var className = "krate-avatar krate-avatar-" + size;

  return (
    <span class={className}>
      {src !== "" && !imageError() ? (
        <img
          class="krate-avatar-image"
          src={src}
          alt={alt}
          onError={handleError}
        />
      ) : null}
      {(src === "" || imageError()) ? (
        <span class="krate-avatar-fallback">
          {fallback}
        </span>
      ) : null}
    </span>
  );
}
