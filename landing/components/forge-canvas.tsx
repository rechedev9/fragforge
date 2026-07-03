"use client";

import { useEffect, useRef, useState } from "react";
import { Canvas, useFrame, useThree } from "@react-three/fiber";
import { EffectComposer, Bloom } from "@react-three/postprocessing";
import * as THREE from "three";

/**
 * ForgeCanvas is the heavy WebGL half of the hero, lazily loaded (ssr:false)
 * only when motion is allowed. It renders the acid-lime "particle forge":
 * thousands of sparks rising and swirling off an unseen anvil below the fold.
 *
 *  - DPR cap comes from the parent (2 on desktop, 1.5 on small viewports).
 *  - Time only advances while the tab is visible (see Interaction), so the
 *    field truly pauses when hidden instead of lurching on resume.
 *  - A lost WebGL context is swallowed, not thrown, so the page never crashes.
 */
export default function ForgeCanvas({
  count,
  dpr,
}: {
  count: number;
  dpr: [number, number];
}) {
  return (
    <Canvas
      className="absolute inset-0"
      dpr={dpr}
      gl={{
        antialias: false,
        alpha: true,
        powerPreference: "high-performance",
        failIfMajorPerformanceCaveat: false,
      }}
      camera={{ position: [0, 0.6, 12], fov: 46, near: 0.1, far: 100 }}
      onCreated={({ gl }) => {
        gl.setClearColor(0x000000, 0);
        const canvas = gl.domElement;
        canvas.addEventListener(
          "webglcontextlost",
          (e) => e.preventDefault(),
          false,
        );
      }}
    >
      <ForgeScene count={count} />
    </Canvas>
  );
}

function ForgeScene({ count }: { count: number }) {
  return (
    <>
      <ForgeGlow />
      <ForgeParticles count={count} />
      <EffectComposer>
        {/* Only the hottest spark cores may bloom; the void stays charcoal. */}
        <Bloom
          intensity={0.5}
          luminanceThreshold={0.65}
          luminanceSmoothing={0.25}
          mipmapBlur
          radius={0.55}
        />
      </EffectComposer>
      <Interaction />
    </>
  );
}

// Shared animation state, written by Interaction and read by both materials.
const flow = {
  time: 0,
  pointerX: 0,
  pointerY: 0,
  scroll: 0,
  scrollTarget: 0,
};

/**
 * Interaction wires pointer parallax and scroll response into `flow` and lerps
 * the camera for a subtle depth parallax. Listeners are passive.
 */
function Interaction() {
  const { camera } = useThree();
  const pointer = useRef({ x: 0, y: 0 });
  const smooth = useRef({ x: 0, y: 0, scroll: 0 });

  useEffect(() => {
    const onPointer = (e: PointerEvent) => {
      pointer.current.x = (e.clientX / window.innerWidth) * 2 - 1;
      pointer.current.y = -((e.clientY / window.innerHeight) * 2 - 1);
    };
    const onScroll = () => {
      const h = window.innerHeight || 1;
      flow.scrollTarget = Math.min(Math.max(window.scrollY / h, 0), 1.4);
    };
    window.addEventListener("pointermove", onPointer, { passive: true });
    window.addEventListener("scroll", onScroll, { passive: true });
    onScroll();
    return () => {
      window.removeEventListener("pointermove", onPointer);
      window.removeEventListener("scroll", onScroll);
    };
  }, []);

  useFrame((_, delta) => {
    // Gate all time accumulation on visibility so a hidden tab truly pauses.
    if (document.visibilityState !== "visible") return;
    const dt = Math.min(delta, 0.05);

    const kPointer = 1 - Math.pow(0.001, dt);
    const kScroll = 1 - Math.pow(0.01, dt);
    const kCam = 1 - Math.pow(0.02, dt);

    smooth.current.x += (pointer.current.x - smooth.current.x) * kPointer;
    smooth.current.y += (pointer.current.y - smooth.current.y) * kPointer;
    smooth.current.scroll += (flow.scrollTarget - smooth.current.scroll) * kScroll;

    flow.time += dt;
    flow.pointerX = smooth.current.x;
    flow.pointerY = smooth.current.y;
    flow.scroll = smooth.current.scroll;

    camera.position.x += (smooth.current.x * 0.8 - camera.position.x) * kCam;
    camera.position.y += (0.6 + smooth.current.y * 0.5 - camera.position.y) * kCam;
    camera.lookAt(0, 0.4, 0);
  });

  return null;
}

function ForgeParticles({ count }: { count: number }) {
  const [geometry] = useState(() => buildGeometry(count));
  const uniforms = useRef({
    uTime: { value: 0 },
    uScroll: { value: 0 },
    uPointer: { value: new THREE.Vector2(0, 0) },
    uPixelRatio: { value: 1 },
    uSize: { value: 3.0 },
  });

  const { gl } = useThree();
  useEffect(() => {
    uniforms.current.uPixelRatio.value = Math.min(gl.getPixelRatio(), 2);
  }, [gl]);

  useEffect(() => {
    const geo = geometry;
    return () => geo.dispose();
  }, [geometry]);

  useFrame(() => {
    const u = uniforms.current;
    u.uTime.value = flow.time;
    u.uScroll.value = flow.scroll;
    u.uPointer.value.set(flow.pointerX, flow.pointerY);
  });

  return (
    <points geometry={geometry} frustumCulled={false}>
      <shaderMaterial
        uniforms={uniforms.current}
        vertexShader={PARTICLE_VERT}
        fragmentShader={PARTICLE_FRAG}
        transparent
        depthWrite={false}
        depthTest={false}
        blending={THREE.AdditiveBlending}
      />
    </points>
  );
}

/**
 * ForgeGlow is a subtle ember glow hugging the bottom edge of the viewport:
 * the plane spans y -10.2..-2.2 at z=-2, where the visible frame ends around
 * y=-5.3, so the peak luminance sits below the fold and only a faint lime
 * tail reaches into the bottom band of the hero. Never behind the headline.
 */
function ForgeGlow() {
  const uniforms = useRef({ uTime: { value: 0 } });
  useFrame(() => {
    uniforms.current.uTime.value = flow.time;
  });
  return (
    <mesh position={[0, -6.2, -2]}>
      <planeGeometry args={[26, 8]} />
      <shaderMaterial
        uniforms={uniforms.current}
        vertexShader={GLOW_VERT}
        fragmentShader={GLOW_FRAG}
        transparent
        depthWrite={false}
        depthTest={false}
        blending={THREE.AdditiveBlending}
      />
    </mesh>
  );
}

function buildGeometry(count: number): THREE.BufferGeometry {
  const positions = new Float32Array(count * 3); // placeholder, animated in shader
  const seed = new Float32Array(count);
  const scale = new Float32Array(count);
  const speed = new Float32Array(count);
  const radius = new Float32Array(count);
  const angle = new Float32Array(count);

  for (let i = 0; i < count; i++) {
    seed[i] = Math.random();
    scale[i] = 0.4 + Math.random() * Math.random() * 1.0;
    speed[i] = 0.3 + Math.random();
    // Bias density toward the axis so the plume tightens near the forge.
    radius[i] = Math.pow(Math.random(), 0.7) * 4.2;
    angle[i] = Math.random() * Math.PI * 2;
  }

  const geo = new THREE.BufferGeometry();
  geo.setAttribute("position", new THREE.BufferAttribute(positions, 3));
  geo.setAttribute("aSeed", new THREE.BufferAttribute(seed, 1));
  geo.setAttribute("aScale", new THREE.BufferAttribute(scale, 1));
  geo.setAttribute("aSpeed", new THREE.BufferAttribute(speed, 1));
  geo.setAttribute("aRadius", new THREE.BufferAttribute(radius, 1));
  geo.setAttribute("aAngle", new THREE.BufferAttribute(angle, 1));
  geo.setDrawRange(0, count);
  return geo;
}

// Ashima 3D simplex noise (webgl-noise, MIT). Drives organic turbulence.
const SIMPLEX_3D = /* glsl */ `
vec3 mod289(vec3 x){return x-floor(x*(1.0/289.0))*289.0;}
vec4 mod289(vec4 x){return x-floor(x*(1.0/289.0))*289.0;}
vec4 permute(vec4 x){return mod289(((x*34.0)+1.0)*x);}
vec4 taylorInvSqrt(vec4 r){return 1.79284291400159-0.85373472095314*r;}
float snoise(vec3 v){
  const vec2 C=vec2(1.0/6.0,1.0/3.0);
  const vec4 D=vec4(0.0,0.5,1.0,2.0);
  vec3 i=floor(v+dot(v,C.yyy));
  vec3 x0=v-i+dot(i,C.xxx);
  vec3 g=step(x0.yzx,x0.xyz);
  vec3 l=1.0-g;
  vec3 i1=min(g.xyz,l.zxy);
  vec3 i2=max(g.xyz,l.zxy);
  vec3 x1=x0-i1+C.xxx;
  vec3 x2=x0-i2+C.yyy;
  vec3 x3=x0-D.yyy;
  i=mod289(i);
  vec4 p=permute(permute(permute(
      i.z+vec4(0.0,i1.z,i2.z,1.0))
    +i.y+vec4(0.0,i1.y,i2.y,1.0))
    +i.x+vec4(0.0,i1.x,i2.x,1.0));
  float n_=0.142857142857;
  vec3 ns=n_*D.wyz-D.xzx;
  vec4 j=p-49.0*floor(p*ns.z*ns.z);
  vec4 x_=floor(j*ns.z);
  vec4 y_=floor(j-7.0*x_);
  vec4 x=x_*ns.x+ns.yyyy;
  vec4 y=y_*ns.x+ns.yyyy;
  vec4 h=1.0-abs(x)-abs(y);
  vec4 b0=vec4(x.xy,y.xy);
  vec4 b1=vec4(x.zw,y.zw);
  vec4 s0=floor(b0)*2.0+1.0;
  vec4 s1=floor(b1)*2.0+1.0;
  vec4 sh=-step(h,vec4(0.0));
  vec4 a0=b0.xzyw+s0.xzyw*sh.xxyy;
  vec4 a1=b1.xzyw+s1.xzyw*sh.zzww;
  vec3 p0=vec3(a0.xy,h.x);
  vec3 p1=vec3(a0.zw,h.y);
  vec3 p2=vec3(a1.xy,h.z);
  vec3 p3=vec3(a1.zw,h.w);
  vec4 norm=taylorInvSqrt(vec4(dot(p0,p0),dot(p1,p1),dot(p2,p2),dot(p3,p3)));
  p0*=norm.x;p1*=norm.y;p2*=norm.z;p3*=norm.w;
  vec4 m=max(0.6-vec4(dot(x0,x0),dot(x1,x1),dot(x2,x2),dot(x3,x3)),0.0);
  m=m*m;
  return 42.0*dot(m*m,vec4(dot(p0,x0),dot(p1,x1),dot(p2,x2),dot(p3,x3)));
}
`;

const PARTICLE_VERT = /* glsl */ `
uniform float uTime;
uniform float uScroll;
uniform vec2 uPointer;
uniform float uPixelRatio;
uniform float uSize;

attribute float aSeed;
attribute float aScale;
attribute float aSpeed;
attribute float aRadius;
attribute float aAngle;

varying float vEnergy;
varying float vAlpha;
varying vec3 vColor;

${SIMPLEX_3D}

void main() {
  float t = uTime;

  // Recycled lifetime 0..1: sparks are born below the fold and rise, faster
  // as the user scrolls past the hero.
  float rate = (0.028 + aSpeed * 0.05) * (1.0 + uScroll * 1.6);
  float life = fract(aSeed + t * rate);

  float yBottom = -6.5;
  float yTop = 7.5;
  float y = mix(yBottom, yTop, life);

  // Vortex swirl: tighter, faster near the axis (the anvil). Radius blooms as
  // the plume rises.
  float swirl = aAngle + (t * 0.22 + life * 6.5) * (0.5 + 0.9 / (aRadius + 0.5));
  float r = aRadius * (0.45 + life * 1.35);

  vec3 pos = vec3(cos(swirl) * r, y, sin(swirl) * r);

  // Organic turbulence via simplex, scrolling downward so the flow reads as
  // heat rising through it.
  vec3 nc = pos * 0.22 + vec3(0.0, -t * 0.13, aSeed * 10.0);
  float n1 = snoise(nc);
  float n2 = snoise(nc + 21.7);
  float turb = 0.35 + life * 0.9;
  pos.x += n1 * turb;
  pos.z += n2 * turb;
  pos.y += snoise(nc * 1.4) * 0.35;

  // Scroll lifts and parallaxes the whole field.
  pos.y += uScroll * 3.2;

  // Subtle pointer parallax, stronger on higher (nearer-feeling) sparks.
  pos.x += uPointer.x * 0.7 * (0.35 + life);
  pos.y += uPointer.y * 0.45 * (0.35 + life);

  vec4 mv = modelViewMatrix * vec4(pos, 1.0);

  // Energy: hottest at the forge (low + near axis), cooling as sparks climb.
  float heightEnergy = 1.0 - smoothstep(-6.5, 3.5, pos.y);
  float coreEnergy = 1.0 - smoothstep(0.0, 3.6, length(pos.xz));
  float energy = clamp(heightEnergy * 0.72 + coreEnergy * 0.5, 0.0, 1.0);
  energy = mix(energy, 1.0, step(0.93, aSeed)); // occasional white-hot ember
  vEnergy = energy;

  // Color ramp: acid lime -> white-hot (linear space; sRGB output applied by
  // the composer). Keeps peak brightness low and off the headline zone.
  vec3 lime = vec3(0.52, 0.90, 0.06);
  vec3 white = vec3(1.0, 1.0, 0.92);
  vColor = mix(lime, white, pow(energy, 1.6));

  float twinkle = 0.62 + 0.38 * sin(t * 3.1 + aSeed * 31.0);
  float lifeFade = smoothstep(0.0, 0.08, life) * (1.0 - smoothstep(0.72, 1.0, life));
  float fog = 1.0 - smoothstep(9.0, 22.0, -mv.z);
  vAlpha = lifeFade * twinkle * fog * (0.45 + energy * 0.65);

  gl_Position = projectionMatrix * mv;
  // Small crisp sparks: a few device pixels at the camera plane (z ~= 12),
  // never fat blobs. The tight fragment falloff leaves a 1-3 px hot core.
  float sizeAtten = 12.0 / max(-mv.z, 0.001);
  gl_PointSize = uSize * aScale * uPixelRatio * (0.75 + energy * 0.5) * sizeAtten;
  gl_PointSize = clamp(gl_PointSize, 1.0, 9.0 * uPixelRatio);
}
`;

const PARTICLE_FRAG = /* glsl */ `
precision highp float;
varying float vEnergy;
varying float vAlpha;
varying vec3 vColor;

void main() {
  // 0..1 radial distance across the (already tiny) sprite quad.
  float d = length(gl_PointCoord - 0.5) * 2.0;
  if (d > 1.0) discard;
  // Tight round falloff: a 1-3 px hot core with a short soft skirt,
  // no broad gaussian halo (that read as blurry blobs).
  float core = pow(1.0 - d, 2.2);
  float a = vAlpha * core;
  // Hottest cores exceed the bloom threshold; cool sparks stay pure lime.
  vec3 col = vColor * (0.85 + vEnergy * 0.9 * core);
  gl_FragColor = vec4(col, a);
}
`;

const GLOW_VERT = /* glsl */ `
varying vec2 vUv;
void main() {
  vUv = uv;
  gl_Position = projectionMatrix * modelViewMatrix * vec4(position, 1.0);
}
`;

const GLOW_FRAG = /* glsl */ `
precision highp float;
uniform float uTime;
varying vec2 vUv;

void main() {
  // Radial falloff anchored low on the (bottom-hugging) plane, so the peak
  // sits below the visible frame and only a dim ember tail reaches the hero.
  vec2 p = vUv - vec2(0.5, 0.2);
  p.x *= 1.5;
  float d = length(p);
  float glow = smoothstep(0.6, 0.0, d);
  glow = pow(glow, 2.4);
  float pulse = 0.92 + 0.08 * sin(uTime * 0.8);
  vec3 lime = vec3(0.42, 0.80, 0.05);
  gl_FragColor = vec4(lime * glow * pulse * 0.4, glow * 0.3);
}
`;
