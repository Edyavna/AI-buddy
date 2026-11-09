import React from 'react';
import clsx from 'clsx';
import styles from './AvitoLogo.module.css';

export interface AvitoLogoProps {
  className?: string;
  showWordmark?: boolean;
  size?: 'sm' | 'md' | 'lg';
}

export const AvitoLogo: React.FC<AvitoLogoProps> = ({
  className,
  showWordmark = true,
  size = 'md',
}) => {
  return (
    <span className={clsx(styles.logoWrapper, styles[size], className)}>
      {/* 4 dots symbol */}
      <svg
        className={styles.dotsSvg}
        viewBox="0 0 30 30"
        xmlns="http://www.w3.org/2000/svg"
        aria-hidden="true"
      >
        <path
          fill="#04E061"
          d="M 8.305 26.167 a 8.305 8.305 0 1 0 0 -16.61 a 8.305 8.305 0 0 0 0 16.61 Z"
        />
        <path
          fill="#FF4053"
          d="M 22.708 25.12 a 5.017 5.017 0 1 0 0 -10.035 a 5.017 5.017 0 0 0 0 10.035 Z"
        />
        <path
          fill="#965EEB"
          d="M 9.892 8.48 a 3.106 3.106 0 1 0 0 -6.211 a 3.106 3.106 0 0 0 0 6.211 Z"
        />
        <path
          fill="#0AF"
          d="M 20.757 14.004 a 6.752 6.752 0 1 0 0 -13.504 a 6.752 6.752 0 0 0 0 13.504 Z"
        />
      </svg>

      {/* Official wordmark */}
      {showWordmark && (
        <svg
          className={styles.wordmarkSvg}
          viewBox="0 0 73 30"
          xmlns="http://www.w3.org/2000/svg"
          aria-hidden="true"
        >
          <path
            fill="currentColor"
            d="M 9.664 1.08 L 0.927 23.891 H 5.62 l 1.796 -4.767 h 9.27 l 1.804 4.767 h 4.658 L 14.465 1.079 h -4.8 Z m -0.637 13.858 l 3.051 -8.026 l 3.04 8.026 H 9.027 Z M 28.757 18.009 l -3.79 -10.143 h -4.476 l 6.103 16.026 h 4.438 l 5.995 -16.026 H 32.55 l -3.793 10.143 Z M 42.658 7.866 h -4.26 v 16.026 h 4.26 V 7.866 Z M 40.526 6.711 a 3.106 3.106 0 1 0 0 -6.211 a 3.106 3.106 0 0 0 0 6.211 Z M 51.102 3.59 h -4.25 V 7.84 h -2.49 V 11.7 h 2.49 v 6.81 c 0 3.863 2.13 5.524 5.127 5.524 a 7.338 7.338 0 0 0 2.947 -0.576 v -3.97 a 4.755 4.755 0 0 1 -1.588 0.289 c -1.302 0 -2.24 -0.506 -2.24 -2.24 V 11.7 h 3.828 V 7.878 h -3.824 V 3.59 Z M 63.883 7.576 a 8.305 8.305 0 1 0 -0.007 16.61 a 8.305 8.305 0 0 0 0.007 -16.61 Z m 0 12.36 a 4.044 4.044 0 1 1 4.04 -4.044 a 4.036 4.036 0 0 1 -4.04 4.029 v 0.015 Z"
          />
        </svg>
      )}

      <span className={styles.srOnly}>Авито</span>
    </span>
  );
};
