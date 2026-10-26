import { useEffect, useRef, useState } from "react";
import { getDocument, GlobalWorkerOptions } from "pdfjs-dist";
import workerUrl from "pdfjs-dist/build/pdf.worker.min.mjs?url";

GlobalWorkerOptions.workerSrc = workerUrl;

export function PdfView({ data }: { data: Uint8Array }) {
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const [pages, setPages] = useState(0);
  const [err, setErr] = useState("");

  useEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas) return;
    let cancelled = false;
    setErr("");
    setPages(0);
    (async () => {
      try {
        const pdf = await getDocument({ data: data.slice(), disableRange: true }).promise;
        if (cancelled) return;
        setPages(pdf.numPages);
        const page = await pdf.getPage(1);
        if (cancelled) return;
        const unscaled = page.getViewport({ scale: 1 });
        const width = canvas.parentElement?.clientWidth ?? 480;
        const scale = Math.min(1.4, Math.max(0.4, (width - 16) / unscaled.width));
        const viewport = page.getViewport({ scale });
        const ctx = canvas.getContext("2d");
        if (!ctx) return;
        canvas.width = viewport.width;
        canvas.height = viewport.height;
        await page.render({ canvasContext: ctx, viewport }).promise;
      } catch (e) {
        if (!cancelled) setErr(e instanceof Error ? e.message : String(e));
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [data]);

  return (
    <div className="tf-pdf">
      <canvas ref={canvasRef} />
      {pages > 1 && <p className="tf-muted">Page 1 of {pages}</p>}
      {err && <p className="tf-danger">PDF canvas failed ({err}). The text extract is below.</p>}
    </div>
  );
}
