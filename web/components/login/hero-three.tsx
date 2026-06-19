'use client';

/**
 * HeroThree — the landing's cinematic 3D motif: a slowly turning film reel of
 * 9:16 highlight frames (the product makes vertical shorts) arcing through cool
 * charcoal space. Whichever frame faces you lights acid-lime — the "chosen
 * play" — so the lime signal travels around the wheel as it turns. Depth fog
 * fades the far side, sparks drift like a forge, and a restrained bloom makes
 * the lime glow. Mouse parallax adds life. Purely decorative; sits full-bleed
 * behind the hero copy. Holds still under prefers-reduced-motion.
 */

import { useMemo, useRef } from 'react';
import { Canvas, useFrame } from '@react-three/fiber';
import { RoundedBox } from '@react-three/drei';
import { EffectComposer, Bloom, Vignette } from '@react-three/postprocessing';
import * as THREE from 'three';

const LIME = '#c4f042';
const CHARCOAL = '#0d0e12';
const FRAME_W = 1.25;
const FRAME_H = (FRAME_W * 16) / 9; // 9:16 short
const COUNT = 16;
const RADIUS = 7;

function prefersReducedMotion() {
  return typeof window !== 'undefined' && window.matchMedia('(prefers-reduced-motion: reduce)').matches;
}

function Frame({ index, rot }: { index: number; rot: { current: number } }) {
  const a = (index / COUNT) * Math.PI * 2;
  const pos: [number, number, number] = [0, Math.sin(a) * RADIUS, Math.cos(a) * RADIUS];

  const body = useRef<THREE.MeshStandardMaterial>(null);
  const screen = useRef<THREE.MeshBasicMaterial>(null);
  const halo = useRef<THREE.MeshBasicMaterial>(null);

  const c = useMemo(
    () => ({
      lime: new THREE.Color(LIME),
      coolEmissive: new THREE.Color('#18202e'),
      darkBody: new THREE.Color('#1b2029'),
      limeBody: new THREE.Color('#1a1f10'),
      darkScreen: new THREE.Color('#2a3240'),
    }),
    [],
  );

  useFrame(() => {
    // How "front-facing" this frame is right now (1 = facing the camera).
    const front = Math.max(0, Math.cos(a + rot.current));
    const sel = THREE.MathUtils.smoothstep(front, 0.84, 1); // only the front 1–2 frames
    if (body.current) {
      body.current.emissive.lerpColors(c.coolEmissive, c.lime, sel);
      body.current.emissiveIntensity = 0.16 + sel * 0.75;
      body.current.color.lerpColors(c.darkBody, c.limeBody, sel);
    }
    if (screen.current) {
      screen.current.color.lerpColors(c.darkScreen, c.lime, sel);
      screen.current.opacity = 0.55 + sel * 0.42;
    }
    if (halo.current) halo.current.opacity = sel * 0.9;
  });

  return (
    <group position={pos} rotation={[-a, 0, 0]}>
      {/* Lime halo — bloom turns this into the glow around the chosen frame. */}
      <mesh position={[0, 0, -0.03]}>
        <planeGeometry args={[FRAME_W * 1.2, FRAME_H * 1.12]} />
        <meshBasicMaterial
          ref={halo}
          color={LIME}
          transparent
          opacity={0}
          blending={THREE.AdditiveBlending}
          depthWrite={false}
        />
      </mesh>

      {/* Frame body. */}
      <RoundedBox args={[FRAME_W, FRAME_H, 0.07]} radius={0.06} smoothness={4}>
        <meshStandardMaterial ref={body} color="#1b2029" metalness={0.4} roughness={0.5} emissive="#18202e" emissiveIntensity={0.16} />
      </RoundedBox>

      {/* Inner "screen". */}
      <mesh position={[0, 0, 0.05]}>
        <planeGeometry args={[FRAME_W * 0.84, FRAME_H * 0.88]} />
        <meshBasicMaterial ref={screen} color="#2a3240" transparent opacity={0.55} />
      </mesh>
    </group>
  );
}

function Sparks() {
  const ref = useRef<THREE.Points>(null);
  const positions = useMemo(() => {
    const n = 240;
    const arr = new Float32Array(n * 3);
    for (let i = 0; i < n; i++) {
      arr[i * 3] = (Math.random() - 0.5) * 26;
      arr[i * 3 + 1] = (Math.random() - 0.5) * 18;
      arr[i * 3 + 2] = (Math.random() - 0.5) * 14 - 1;
    }
    return arr;
  }, []);

  useFrame((_, dt) => {
    const p = ref.current;
    if (!p) return;
    const arr = p.geometry.attributes.position.array as Float32Array;
    for (let i = 1; i < arr.length; i += 3) {
      arr[i] += dt * 0.28; // embers drift up
      if (arr[i] > 9) arr[i] = -9;
    }
    p.geometry.attributes.position.needsUpdate = true;
  });

  return (
    <points ref={ref}>
      <bufferGeometry>
        <bufferAttribute attach="attributes-position" args={[positions, 3]} />
      </bufferGeometry>
      <pointsMaterial size={0.04} color={LIME} transparent opacity={0.55} sizeAttenuation blending={THREE.AdditiveBlending} depthWrite={false} />
    </points>
  );
}

function Reel() {
  const outer = useRef<THREE.Group>(null); // tilt + parallax (no spin)
  const inner = useRef<THREE.Group>(null); // the turning reel
  const limeLight = useRef<THREE.PointLight>(null);
  const rot = useRef(0);
  const reduced = useMemo(prefersReducedMotion, []);
  const frames = useMemo(() => Array.from({ length: COUNT }, (_, i) => i), []);

  useFrame((state, dt) => {
    if (!reduced) rot.current += dt * 0.1; // the reel turns
    if (inner.current) inner.current.rotation.x = rot.current;

    if (outer.current) {
      const px = state.pointer.x;
      const py = state.pointer.y;
      outer.current.rotation.y = THREE.MathUtils.lerp(outer.current.rotation.y, -0.48 + px * 0.16, 0.05);
      outer.current.rotation.z = THREE.MathUtils.lerp(outer.current.rotation.z, 0.16 + py * 0.05, 0.05);
    }
    if (limeLight.current && !reduced) {
      limeLight.current.intensity = 11 + Math.sin(state.clock.elapsedTime * 1.6) * 2.5;
    }
  });

  // Offset right so the hero copy keeps the left clear.
  return (
    <group ref={outer} position={[3, -0.2, 0]} rotation={[0, -0.48, 0.16]}>
      <group ref={inner}>
        {frames.map((i) => (
          <Frame key={i} index={i} rot={rot} />
        ))}
      </group>
      {/* Fixed at the wheel's front so the facing frame is lime-lit. */}
      <pointLight ref={limeLight} position={[0, 0, RADIUS + 1]} color={LIME} intensity={11} distance={18} decay={2} />
    </group>
  );
}

export default function HeroThree({ className }: { className?: string }) {
  return (
    <div className={className} aria-hidden>
      <Canvas
        dpr={[1, 1.8]}
        gl={{ antialias: true, alpha: true, powerPreference: 'high-performance' }}
        camera={{ position: [0, 0, 17], fov: 36 }}
      >
        <fog attach="fog" args={[CHARCOAL, 12, 26]} />
        <ambientLight intensity={0.4} />
        <directionalLight position={[6, 8, 6]} intensity={1.0} color="#cfd6e6" />
        <directionalLight position={[-8, -2, 4]} intensity={0.35} color="#3a4a6a" />
        <Reel />
        <Sparks />
        <EffectComposer enableNormalPass={false}>
          <Bloom intensity={1.5} luminanceThreshold={0.5} luminanceSmoothing={0.25} mipmapBlur radius={0.78} />
          <Vignette eskil={false} offset={0.22} darkness={0.82} />
        </EffectComposer>
      </Canvas>
    </div>
  );
}
